package payments

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type XunhuConfig struct {
	Enabled     bool
	AppID       string
	Secret      string
	NotifyURL   string
	ReturnURL   string
	CallbackURL string
	Gateway     string
	QueryURL    string
}

type XunhuClient struct {
	cfg    XunhuConfig
	client *http.Client
}

func NewXunhuClient(cfg XunhuConfig) (*XunhuClient, error) {
	return &XunhuClient{
		cfg: cfg,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}, nil
}

func (c *XunhuClient) Enabled() bool {
	return c != nil && c.cfg.Enabled && strings.TrimSpace(c.cfg.AppID) != "" && strings.TrimSpace(c.cfg.Secret) != ""
}

func (c *XunhuClient) CreateOrder(ctx context.Context, req CreateOrderRequest) (*CreateOrderResult, error) {
	form := url.Values{}
	form.Set("version", "1.1")
	form.Set("appid", strings.TrimSpace(c.cfg.AppID))
	form.Set("trade_order_id", req.OrderNo)
	form.Set("total_fee", formatAmountYuan(req.Amount))
	form.Set("title", truncateTitle(req.Description))
	form.Set("time", strconv.FormatInt(time.Now().Unix(), 10))
	form.Set("notify_url", firstXunhuNonEmpty(c.cfg.NotifyURL, req.NotifyURL))
	if returnURL := firstXunhuNonEmpty(c.cfg.ReturnURL, req.ReturnURL); returnURL != "" {
		form.Set("return_url", returnURL)
	}
	if callbackURL := firstXunhuNonEmpty(c.cfg.CallbackURL, req.CallbackURL); callbackURL != "" {
		form.Set("callback_url", callbackURL)
	}
	form.Set("plugins", "CLIProxyCloud")
	form.Set("attach", req.OrderNo)
	form.Set("nonce_str", randomNonce(24))
	form.Set("hash", xunhuSign(form, c.cfg.Secret))

	body, err := c.postForm(ctx, c.cfg.Gateway, form)
	if err != nil {
		return nil, err
	}

	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("decode xunhu create response: %w", err)
	}
	if err := verifyXunhuPayload(data, c.cfg.Secret); err != nil {
		return nil, err
	}
	errCode, _ := asInt64(data["errcode"])
	if errCode != 0 {
		return nil, fmt.Errorf("xunhu create order failed: %s", strings.TrimSpace(asString(data["errmsg"])))
	}

	return &CreateOrderResult{
		ProviderOrderID: asString(data["openid"]),
		CodeURL:         strings.TrimSpace(asString(data["url_qrcode"])),
		Raw:             body,
	}, nil
}

func (c *XunhuClient) QueryOrder(ctx context.Context, orderNo string) (*QueryOrderResult, error) {
	form := url.Values{}
	form.Set("appid", strings.TrimSpace(c.cfg.AppID))
	form.Set("out_trade_order", orderNo)
	form.Set("time", strconv.FormatInt(time.Now().Unix(), 10))
	form.Set("nonce_str", randomNonce(24))
	form.Set("hash", xunhuSign(form, c.cfg.Secret))

	body, err := c.postForm(ctx, c.cfg.QueryURL, form)
	if err != nil {
		return nil, err
	}

	var data struct {
		ErrCode int64                  `json:"errcode"`
		ErrMsg  string                 `json:"errmsg"`
		Hash    string                 `json:"hash"`
		Data    map[string]any         `json:"data"`
		Raw     map[string]interface{} `json:"-"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("decode xunhu query response: %w", err)
	}
	generic := map[string]any{
		"errcode": data.ErrCode,
		"errmsg":  data.ErrMsg,
		"hash":    data.Hash,
	}
	if data.Data != nil {
		generic["data"] = data.Data
	}
	if err := verifyXunhuPayload(generic, c.cfg.Secret); err != nil {
		return nil, err
	}
	if data.ErrCode != 0 {
		return nil, fmt.Errorf("xunhu query failed: %s", strings.TrimSpace(data.ErrMsg))
	}

	status := strings.TrimSpace(asString(data.Data["status"]))
	return &QueryOrderResult{
		ProviderOrderID: asString(data.Data["open_order_id"]),
		ProviderTradeNo: asString(data.Data["transaction_id"]),
		Status:          mapXunhuQueryStatus(status),
		PaidAt:          nil,
		Raw:             body,
	}, nil
}

func (c *XunhuClient) ParseNotify(values url.Values) (*NotifyResult, error) {
	data := make(map[string]any)
	for key, items := range values {
		if len(items) > 0 {
			data[key] = items[0]
		}
	}
	if err := verifyXunhuPayload(data, c.cfg.Secret); err != nil {
		return nil, err
	}
	orderNo := strings.TrimSpace(values.Get("trade_order_id"))
	if orderNo == "" {
		return nil, fmt.Errorf("xunhu notify missing trade_order_id")
	}
	raw, _ := json.Marshal(data)
	now := time.Now()
	result := &NotifyResult{
		OrderNo:         orderNo,
		ProviderOrderID: strings.TrimSpace(values.Get("open_order_id")),
		ProviderTradeNo: strings.TrimSpace(values.Get("transaction_id")),
		Status:          mapXunhuNotifyStatus(strings.TrimSpace(values.Get("status"))),
		Raw:             raw,
	}
	if result.Status == "paid" {
		result.PaidAt = &now
	}
	return result, nil
}

func (c *XunhuClient) postForm(ctx context.Context, endpoint string, form url.Values) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(endpoint), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build xunhu request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")

	response, err := c.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("xunhu request failed: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read xunhu response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("xunhu request failed: %s", strings.TrimSpace(string(body)))
	}
	return body, nil
}

func xunhuSign(values map[string][]string, secret string) string {
	flat := make(map[string]string, len(values))
	for key, items := range values {
		if len(items) > 0 {
			flat[key] = items[0]
		}
	}
	return xunhuSignMap(flat, secret)
}

func xunhuSignMap(values map[string]string, secret string) string {
	keys := make([]string, 0, len(values))
	for key, value := range values {
		if key == "hash" || strings.TrimSpace(value) == "" {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values[key])
	}
	sum := md5.Sum([]byte(strings.Join(parts, "&") + secret))
	return hex.EncodeToString(sum[:])
}

func verifyXunhuPayload(payload map[string]any, secret string) error {
	got := strings.TrimSpace(asString(payload["hash"]))
	if got == "" {
		return fmt.Errorf("xunhu payload missing hash")
	}
	flat := make(map[string]string, len(payload))
	for key, value := range payload {
		if key == "data" {
			nested, ok := value.(map[string]any)
			if !ok {
				continue
			}
			for nestedKey, nestedValue := range nested {
				flat[nestedKey] = asString(nestedValue)
			}
			continue
		}
		flat[key] = asString(value)
	}
	expected := xunhuSignMap(flat, secret)
	if !strings.EqualFold(expected, got) {
		return fmt.Errorf("invalid xunhu signature")
	}
	return nil
}

func mapXunhuQueryStatus(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "OD":
		return "paid"
	case "CD":
		return "closed"
	case "WP":
		return "pending"
	default:
		return "pending"
	}
}

func mapXunhuNotifyStatus(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "OD":
		return "paid"
	case "CD", "RD":
		return "refunded"
	case "UD":
		return "failed"
	default:
		return "pending"
	}
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		if math.Trunc(typed) == typed {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

func asInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case float64:
		return int64(typed), true
	case int:
		return int64(typed), true
	case json.Number:
		v, err := typed.Int64()
		return v, err == nil
	case string:
		v, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return v, err == nil
	default:
		return 0, false
	}
}

func formatAmountYuan(amountCents int64) string {
	return strconv.FormatFloat(float64(amountCents)/100, 'f', 2, 64)
}

func truncateTitle(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "CLIProxy 会员支付"
	}
	runes := []rune(trimmed)
	if len(runes) > 42 {
		return string(runes[:42])
	}
	return trimmed
}

func randomNonce(length int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	if length <= 0 {
		length = 24
	}
	b := make([]byte, length)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(b)
}

func firstXunhuNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
