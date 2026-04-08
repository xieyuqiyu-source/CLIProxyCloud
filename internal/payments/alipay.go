package payments

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type AlipayConfig struct {
	Enabled            bool
	AppID              string
	PrivateKeyPEM      string
	PrivateKeyPath     string
	AlipayPublicKeyPEM string
	NotifyURL          string
	Gateway            string
}

type AlipayClient struct {
	cfg        AlipayConfig
	httpClient *http.Client
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
}

func NewAlipayClient(cfg AlipayConfig) (*AlipayClient, error) {
	if !cfg.Enabled {
		return &AlipayClient{cfg: cfg}, nil
	}
	privateKey, err := loadRSAPrivateKey(cfg.PrivateKeyPEM, cfg.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load alipay private key: %w", err)
	}
	publicKey, err := loadRSAPublicKeyFromCertOrKey(cfg.AlipayPublicKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("load alipay public key: %w", err)
	}
	gateway := strings.TrimSpace(cfg.Gateway)
	if gateway == "" {
		gateway = "https://openapi.alipay.com/gateway.do"
	}
	return &AlipayClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 20 * time.Second},
		privateKey: privateKey,
		publicKey:  publicKey,
	}, nil
}

func (c *AlipayClient) Enabled() bool {
	return c != nil && c.cfg.Enabled
}

func (c *AlipayClient) CreatePrecreateOrder(ctx context.Context, req CreateOrderRequest) (*CreateOrderResult, error) {
	bizContent, _ := json.Marshal(map[string]any{
		"out_trade_no":    req.OrderNo,
		"total_amount":    amountToYuan(req.Amount),
		"subject":         req.Description,
		"timeout_express": "15m",
	})
	params := url.Values{}
	params.Set("app_id", c.cfg.AppID)
	params.Set("method", "alipay.trade.precreate")
	params.Set("format", "JSON")
	params.Set("charset", "utf-8")
	params.Set("sign_type", "RSA2")
	params.Set("timestamp", time.Now().Format("2006-01-02 15:04:05"))
	params.Set("version", "1.0")
	params.Set("notify_url", firstNonEmpty(c.cfg.NotifyURL, req.NotifyURL))
	params.Set("biz_content", string(bizContent))
	signature, err := c.sign(params)
	if err != nil {
		return nil, err
	}
	params.Set("sign", signature)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Gateway, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("alipay request failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("alipay request failed: %s", strings.TrimSpace(string(body)))
	}
	var data struct {
		Response struct {
			Code       string `json:"code"`
			Msg        string `json:"msg"`
			SubMsg     string `json:"sub_msg"`
			OutTradeNo string `json:"out_trade_no"`
			QRCode     string `json:"qr_code"`
		} `json:"alipay_trade_precreate_response"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("decode alipay precreate response: %w", err)
	}
	if data.Response.Code != "10000" || data.Response.QRCode == "" {
		return nil, fmt.Errorf("alipay precreate failed: %s %s", data.Response.Msg, data.Response.SubMsg)
	}
	return &CreateOrderResult{
		ProviderOrderID: data.Response.OutTradeNo,
		CodeURL:         data.Response.QRCode,
		Raw:             body,
	}, nil
}

func (c *AlipayClient) QueryOrder(ctx context.Context, orderNo string) (*QueryOrderResult, error) {
	bizContent, _ := json.Marshal(map[string]any{
		"out_trade_no": orderNo,
	})
	params := url.Values{}
	params.Set("app_id", c.cfg.AppID)
	params.Set("method", "alipay.trade.query")
	params.Set("format", "JSON")
	params.Set("charset", "utf-8")
	params.Set("sign_type", "RSA2")
	params.Set("timestamp", time.Now().Format("2006-01-02 15:04:05"))
	params.Set("version", "1.0")
	params.Set("biz_content", string(bizContent))
	signature, err := c.sign(params)
	if err != nil {
		return nil, err
	}
	params.Set("sign", signature)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Gateway, strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("alipay query failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var data struct {
		Response struct {
			Code        string `json:"code"`
			Msg         string `json:"msg"`
			SubMsg      string `json:"sub_msg"`
			OutTradeNo  string `json:"out_trade_no"`
			TradeNo     string `json:"trade_no"`
			TradeStatus string `json:"trade_status"`
			SendPayDate string `json:"send_pay_date"`
		} `json:"alipay_trade_query_response"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("decode alipay query response: %w", err)
	}
	if data.Response.Code != "10000" {
		return nil, fmt.Errorf("alipay query failed: %s %s", data.Response.Msg, data.Response.SubMsg)
	}
	var paidAt *time.Time
	if data.Response.SendPayDate != "" {
		if parsed, err := time.Parse("2006-01-02 15:04:05", data.Response.SendPayDate); err == nil {
			paidAt = &parsed
		}
	}
	return &QueryOrderResult{
		ProviderOrderID: data.Response.OutTradeNo,
		ProviderTradeNo: data.Response.TradeNo,
		Status:          mapAlipayTradeState(data.Response.TradeStatus),
		PaidAt:          paidAt,
		Raw:             body,
	}, nil
}

func (c *AlipayClient) ParseNotify(values url.Values) (*NotifyResult, error) {
	sign := values.Get("sign")
	if sign == "" {
		return nil, fmt.Errorf("missing alipay sign")
	}
	if err := c.verify(values); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(values)
	var paidAt *time.Time
	if payTime := values.Get("gmt_payment"); payTime != "" {
		if parsed, err := time.Parse("2006-01-02 15:04:05", payTime); err == nil {
			paidAt = &parsed
		}
	}
	return &NotifyResult{
		OrderNo:         values.Get("out_trade_no"),
		ProviderOrderID: values.Get("out_trade_no"),
		ProviderTradeNo: values.Get("trade_no"),
		Status:          mapAlipayTradeState(values.Get("trade_status")),
		PaidAt:          paidAt,
		Raw:             raw,
	}, nil
}

func (c *AlipayClient) sign(values url.Values) (string, error) {
	content := buildAlipaySignContent(values, true)
	hashed := sha256.Sum256([]byte(content))
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.privateKey, crypto.SHA256, hashed[:])
	if err != nil {
		return "", fmt.Errorf("sign alipay request: %w", err)
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

func (c *AlipayClient) verify(values url.Values) error {
	content := buildAlipaySignContent(values, false)
	signature, err := base64.StdEncoding.DecodeString(values.Get("sign"))
	if err != nil {
		return fmt.Errorf("decode alipay sign: %w", err)
	}
	hashed := sha256.Sum256([]byte(content))
	if err := rsa.VerifyPKCS1v15(c.publicKey, crypto.SHA256, hashed[:], signature); err != nil {
		return fmt.Errorf("verify alipay sign: %w", err)
	}
	return nil
}

func buildAlipaySignContent(values url.Values, includeEmptySign bool) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key == "sign" {
			continue
		}
		if key == "sign_type" {
			continue
		}
		if !includeEmptySign && len(values.Get(key)) == 0 {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values.Get(key))
	}
	return strings.Join(parts, "&")
}

func amountToYuan(amount int64) string {
	return fmt.Sprintf("%.2f", float64(amount)/100.0)
}

func mapAlipayTradeState(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "TRADE_SUCCESS", "TRADE_FINISHED":
		return "paid"
	case "TRADE_CLOSED":
		return "closed"
	default:
		return "pending"
	}
}

func loadAlipayPublicKey(pemContent string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(pemContent)))
	if block == nil {
		return nil, fmt.Errorf("invalid PEM content")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is not RSA")
	}
	return key, nil
}
