package payments

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
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
	"os"
	"strings"
	"time"
)

type WeChatConfig struct {
	Enabled          bool
	AppID            string
	MchID            string
	SerialNo         string
	PrivateKeyPEM    string
	PrivateKeyPath   string
	APIV3Key         string
	NotifyURL        string
	Gateway          string
	PlatformCertPEM  string
	PlatformSerialNo string
}

type WeChatClient struct {
	cfg        WeChatConfig
	httpClient *http.Client
	privateKey *rsa.PrivateKey
	verifyKey  *rsa.PublicKey
}

func NewWeChatClient(cfg WeChatConfig) (*WeChatClient, error) {
	if !cfg.Enabled {
		return &WeChatClient{cfg: cfg}, nil
	}
	privateKey, err := loadRSAPrivateKey(cfg.PrivateKeyPEM, cfg.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load wechat private key: %w", err)
	}
	var verifyKey *rsa.PublicKey
	if strings.TrimSpace(cfg.PlatformCertPEM) != "" {
		verifyKey, err = loadRSAPublicKeyFromCertOrKey(cfg.PlatformCertPEM)
		if err != nil {
			return nil, fmt.Errorf("load wechat platform cert: %w", err)
		}
	}
	gateway := strings.TrimRight(strings.TrimSpace(cfg.Gateway), "/")
	if gateway == "" {
		gateway = "https://api.mch.weixin.qq.com"
	}
	return &WeChatClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 20 * time.Second},
		privateKey: privateKey,
		verifyKey:  verifyKey,
	}, nil
}

func (c *WeChatClient) Enabled() bool {
	return c != nil && c.cfg.Enabled
}

func (c *WeChatClient) CreateNativeOrder(ctx context.Context, req CreateOrderRequest) (*CreateOrderResult, error) {
	bodyMap := map[string]any{
		"appid":        c.cfg.AppID,
		"mchid":        c.cfg.MchID,
		"description":  req.Description,
		"out_trade_no": req.OrderNo,
		"notify_url":   firstNonEmpty(c.cfg.NotifyURL, req.NotifyURL),
		"amount": map[string]any{
			"total":    req.Amount,
			"currency": firstNonEmpty(req.Currency, "CNY"),
		},
	}
	body, _ := json.Marshal(bodyMap)
	respBody, err := c.doSignedRequest(ctx, http.MethodPost, "/v3/pay/transactions/native", "", body)
	if err != nil {
		return nil, err
	}
	var data struct {
		CodeURL string `json:"code_url"`
	}
	if err := json.Unmarshal(respBody, &data); err != nil {
		return nil, fmt.Errorf("decode wechat native response: %w", err)
	}
	if data.CodeURL == "" {
		return nil, fmt.Errorf("wechat native response missing code_url")
	}
	return &CreateOrderResult{
		ProviderOrderID: req.OrderNo,
		CodeURL:         data.CodeURL,
		Raw:             respBody,
	}, nil
}

func (c *WeChatClient) QueryOrder(ctx context.Context, orderNo string) (*QueryOrderResult, error) {
	query := url.Values{}
	query.Set("mchid", c.cfg.MchID)
	path := "/v3/pay/transactions/out-trade-no/" + url.PathEscape(orderNo)
	respBody, err := c.doSignedRequest(ctx, http.MethodGet, path, query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var data struct {
		OutTradeNo    string `json:"out_trade_no"`
		TransactionID string `json:"transaction_id"`
		TradeState    string `json:"trade_state"`
		SuccessTime   string `json:"success_time"`
	}
	if err := json.Unmarshal(respBody, &data); err != nil {
		return nil, fmt.Errorf("decode wechat query response: %w", err)
	}
	var paidAt *time.Time
	if data.SuccessTime != "" {
		if parsed, err := time.Parse(time.RFC3339, data.SuccessTime); err == nil {
			paidAt = &parsed
		}
	}
	return &QueryOrderResult{
		ProviderOrderID: data.OutTradeNo,
		ProviderTradeNo: data.TransactionID,
		Status:          mapWeChatTradeState(data.TradeState),
		PaidAt:          paidAt,
		Raw:             respBody,
	}, nil
}

func (c *WeChatClient) ParseNotify(headers http.Header, body []byte) (*NotifyResult, error) {
	if c.verifyKey != nil {
		if err := c.verifyNotify(headers, body); err != nil {
			return nil, err
		}
	}
	var envelope struct {
		ID       string `json:"id"`
		Resource struct {
			Algorithm      string `json:"algorithm"`
			Ciphertext     string `json:"ciphertext"`
			AssociatedData string `json:"associated_data"`
			Nonce          string `json:"nonce"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode wechat notify body: %w", err)
	}
	plaintext, err := decryptWechatResource(c.cfg.APIV3Key, envelope.Resource.AssociatedData, envelope.Resource.Nonce, envelope.Resource.Ciphertext)
	if err != nil {
		return nil, err
	}
	var payload struct {
		OutTradeNo    string `json:"out_trade_no"`
		TransactionID string `json:"transaction_id"`
		TradeState    string `json:"trade_state"`
		SuccessTime   string `json:"success_time"`
	}
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		return nil, fmt.Errorf("decode wechat notify resource: %w", err)
	}
	var paidAt *time.Time
	if payload.SuccessTime != "" {
		if parsed, err := time.Parse(time.RFC3339, payload.SuccessTime); err == nil {
			paidAt = &parsed
		}
	}
	return &NotifyResult{
		OrderNo:         payload.OutTradeNo,
		ProviderOrderID: payload.OutTradeNo,
		ProviderTradeNo: payload.TransactionID,
		Status:          mapWeChatTradeState(payload.TradeState),
		PaidAt:          paidAt,
		Raw:             plaintext,
	}, nil
}

func (c *WeChatClient) doSignedRequest(ctx context.Context, method, path, rawQuery string, body []byte) ([]byte, error) {
	reqURL := strings.TrimRight(c.cfg.Gateway, "/") + path
	if rawQuery != "" {
		reqURL += "?" + rawQuery
	}
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonce := randomString(32)
	message := fmt.Sprintf("%s\n%s\n%s\n%s\n%s\n", method, pathWithQuery(path, rawQuery), timestamp, nonce, string(body))
	hashed := sha256.Sum256([]byte(message))
	signature, err := rsa.SignPKCS1v15(rand.Reader, c.privateKey, crypto.SHA256, hashed[:])
	if err != nil {
		return nil, fmt.Errorf("sign wechat request: %w", err)
	}
	authHeader := fmt.Sprintf(
		`WECHATPAY2-SHA256-RSA2048 mchid="%s",nonce_str="%s",signature="%s",timestamp="%s",serial_no="%s"`,
		c.cfg.MchID,
		nonce,
		base64.StdEncoding.EncodeToString(signature),
		timestamp,
		c.cfg.SerialNo,
	)
	httpReq, err := http.NewRequestWithContext(ctx, method, reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", authHeader)
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "CLIProxyCloud/1.0")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("wechat request failed: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("wechat request failed: %s", strings.TrimSpace(string(respBody)))
	}
	return respBody, nil
}

func (c *WeChatClient) verifyNotify(headers http.Header, body []byte) error {
	signature := headers.Get("Wechatpay-Signature")
	timestamp := headers.Get("Wechatpay-Timestamp")
	nonce := headers.Get("Wechatpay-Nonce")
	serial := headers.Get("Wechatpay-Serial")
	if signature == "" || timestamp == "" || nonce == "" {
		return fmt.Errorf("missing wechat notify signature headers")
	}
	if c.cfg.PlatformSerialNo != "" && serial != "" && serial != c.cfg.PlatformSerialNo {
		return fmt.Errorf("wechat platform serial mismatch")
	}
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("decode wechat notify signature: %w", err)
	}
	message := fmt.Sprintf("%s\n%s\n%s\n", timestamp, nonce, string(body))
	hashed := sha256.Sum256([]byte(message))
	if err := rsa.VerifyPKCS1v15(c.verifyKey, crypto.SHA256, hashed[:], sig); err != nil {
		return fmt.Errorf("verify wechat notify signature: %w", err)
	}
	return nil
}

func decryptWechatResource(apiV3Key, associatedData, nonce, ciphertext string) ([]byte, error) {
	if len(apiV3Key) != 32 {
		return nil, fmt.Errorf("invalid wechat api v3 key length")
	}
	cipherBytes, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decode wechat ciphertext: %w", err)
	}
	block, err := aes.NewCipher([]byte(apiV3Key))
	if err != nil {
		return nil, fmt.Errorf("create aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create gcm cipher: %w", err)
	}
	plain, err := gcm.Open(nil, []byte(nonce), cipherBytes, []byte(associatedData))
	if err != nil {
		return nil, fmt.Errorf("decrypt wechat resource: %w", err)
	}
	return plain, nil
}

func mapWeChatTradeState(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "SUCCESS":
		return "paid"
	case "CLOSED", "REVOKED":
		return "closed"
	case "PAYERROR":
		return "failed"
	default:
		return "pending"
	}
}

func pathWithQuery(path, rawQuery string) string {
	if rawQuery == "" {
		return path
	}
	return path + "?" + rawQuery
}

func randomString(length int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	for i := range buf {
		buf[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	return string(buf)
}

func loadRSAPrivateKey(pemContent, filePath string) (*rsa.PrivateKey, error) {
	raw := strings.TrimSpace(pemContent)
	if raw == "" && strings.TrimSpace(filePath) != "" {
		bytes, err := os.ReadFile(strings.TrimSpace(filePath))
		if err != nil {
			return nil, err
		}
		raw = string(bytes)
	}
	block, _ := pem.Decode([]byte(raw))
	if block == nil {
		return nil, fmt.Errorf("invalid PEM content")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	pkcs8, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := pkcs8.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not RSA")
	}
	return key, nil
}

func loadRSAPublicKeyFromCertOrKey(pemContent string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(pemContent)))
	if block == nil {
		return nil, fmt.Errorf("invalid PEM content")
	}
	if cert, err := x509.ParseCertificate(block.Bytes); err == nil {
		if key, ok := cert.PublicKey.(*rsa.PublicKey); ok {
			return key, nil
		}
		return nil, fmt.Errorf("certificate public key is not RSA")
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
