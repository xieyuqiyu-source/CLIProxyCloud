package services

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/config"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/models"
	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/payments"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type PaymentProductInput struct {
	ProductCode  string
	Name         string
	DisplayName  string
	PlanCode     string
	PriceAmount  int64
	Currency     string
	DurationDays int
	Status       models.PaymentProductStatus
	SortOrder    int
	Description  string
}

type PaymentCheckout struct {
	Provider        models.PaymentProvider `json:"provider"`
	PaymentEnabled  bool                   `json:"paymentEnabled"`
	CodeURL         string                 `json:"codeUrl,omitempty"`
	ProviderOrderID string                 `json:"providerOrderId,omitempty"`
	ProviderTradeNo string                 `json:"providerTradeNo,omitempty"`
	Message         string                 `json:"message,omitempty"`
}

type PaymentService struct {
	db     *gorm.DB
	plan   *PlanService
	wechat *payments.WeChatClient
	alipay *payments.AlipayClient
	cfg    config.PaymentConfig
}

func NewPaymentService(db *gorm.DB, planSvc *PlanService, cfg config.PaymentConfig) (*PaymentService, error) {
	wechatClient, err := payments.NewWeChatClient(payments.WeChatConfig{
		Enabled:          cfg.WeChat.Enabled,
		AppID:            cfg.WeChat.AppID,
		MchID:            cfg.WeChat.MchID,
		SerialNo:         cfg.WeChat.SerialNo,
		PrivateKeyPEM:    cfg.WeChat.PrivateKeyPEM,
		PrivateKeyPath:   cfg.WeChat.PrivateKeyPath,
		APIV3Key:         cfg.WeChat.APIV3Key,
		NotifyURL:        cfg.WeChat.NotifyURL,
		Gateway:          cfg.WeChat.Gateway,
		PlatformCertPEM:  cfg.WeChat.PlatformCertPEM,
		PlatformSerialNo: cfg.WeChat.PlatformSerialNo,
	})
	if err != nil {
		return nil, err
	}
	alipayClient, err := payments.NewAlipayClient(payments.AlipayConfig{
		Enabled:            cfg.Alipay.Enabled,
		AppID:              cfg.Alipay.AppID,
		PrivateKeyPEM:      cfg.Alipay.PrivateKeyPEM,
		PrivateKeyPath:     cfg.Alipay.PrivateKeyPath,
		AlipayPublicKeyPEM: cfg.Alipay.AlipayPublicKeyPEM,
		NotifyURL:          cfg.Alipay.NotifyURL,
		Gateway:            cfg.Alipay.Gateway,
	})
	if err != nil {
		return nil, err
	}
	return &PaymentService{db: db, plan: planSvc, wechat: wechatClient, alipay: alipayClient, cfg: cfg}, nil
}

func DefaultPaymentProducts() []PaymentProductInput {
	return []PaymentProductInput{
		{
			ProductCode:  "pro_monthly",
			Name:         "Pro Monthly",
			DisplayName:  "Pro 月付",
			PlanCode:     "vip1",
			PriceAmount:  0,
			Currency:     "CNY",
			DurationDays: 30,
			Status:       models.PaymentProductStatusActive,
			SortOrder:    10,
			Description:  "可开通 Pro 套餐，支持自动切换、个人云同步、共享号池随机 3 个。",
		},
		{
			ProductCode:  "pro_max_monthly",
			Name:         "Pro Max Monthly",
			DisplayName:  "Pro Max 月付",
			PlanCode:     "vip2",
			PriceAmount:  0,
			Currency:     "CNY",
			DurationDays: 30,
			Status:       models.PaymentProductStatusActive,
			SortOrder:    20,
			Description:  "可开通 Pro Max 套餐，支持完整共享号池、自动切换和个人云同步。",
		},
	}
}

func (s *PaymentService) SeedDefaults() error {
	for _, product := range DefaultPaymentProducts() {
		if _, err := s.UpsertProduct(product.ProductCode, product); err != nil {
			return err
		}
	}
	return nil
}

func (s *PaymentService) ListAdminProducts() ([]models.PaymentProduct, error) {
	var products []models.PaymentProduct
	err := s.db.Order("sort_order asc, id asc").Find(&products).Error
	return products, err
}

func (s *PaymentService) ListEnabledProducts() ([]models.PaymentProduct, error) {
	var products []models.PaymentProduct
	err := s.db.Where("status = ?", models.PaymentProductStatusActive).Order("sort_order asc, id asc").Find(&products).Error
	return products, err
}

func (s *PaymentService) FindProductByID(id uint) (*models.PaymentProduct, error) {
	var product models.PaymentProduct
	if err := s.db.First(&product, id).Error; err != nil {
		return nil, err
	}
	return &product, nil
}

func (s *PaymentService) FindProductByCode(productCode string) (*models.PaymentProduct, error) {
	var product models.PaymentProduct
	if err := s.db.Where("product_code = ?", normalizeProductCode(productCode)).First(&product).Error; err != nil {
		return nil, err
	}
	return &product, nil
}

func (s *PaymentService) FindOrderByNo(orderNo string) (*models.PaymentOrder, error) {
	var order models.PaymentOrder
	if err := s.db.Where("order_no = ?", strings.TrimSpace(orderNo)).First(&order).Error; err != nil {
		return nil, err
	}
	return &order, nil
}

func (s *PaymentService) ListAdminOrders(limit int) ([]models.PaymentOrder, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var orders []models.PaymentOrder
	err := s.db.Order("id desc").Limit(limit).Find(&orders).Error
	return orders, err
}

func (s *PaymentService) CreateOrder(ctx context.Context, userID uint, productCode string, provider models.PaymentProvider) (*models.PaymentOrder, *models.PaymentProduct, *PaymentCheckout, error) {
	if provider != models.PaymentProviderWeChat && provider != models.PaymentProviderAlipay {
		return nil, nil, nil, fmt.Errorf("unsupported payment provider")
	}
	product, err := s.FindProductByCode(productCode)
	if err != nil {
		return nil, nil, nil, err
	}
	if product.Status != models.PaymentProductStatusActive {
		return nil, nil, nil, fmt.Errorf("payment product is disabled")
	}

	expiresAt := time.Now().Add(15 * time.Minute)
	order := &models.PaymentOrder{
		OrderNo:         newOrderNo(),
		UserID:          userID,
		ProductID:       product.ID,
		PlanCode:        product.PlanCode,
		PaymentProvider: provider,
		Amount:          product.PriceAmount,
		Currency:        product.Currency,
		Status:          models.PaymentOrderStatusPending,
		ExpiresAt:       &expiresAt,
	}
	if err := s.db.Create(order).Error; err != nil {
		return nil, nil, nil, err
	}

	checkout := &PaymentCheckout{
		Provider:       provider,
		PaymentEnabled: false,
		Message:        "payment provider integration is not configured",
	}

	createReq := payments.CreateOrderRequest{
		OrderNo:     order.OrderNo,
		Description: product.DisplayName,
		Amount:      order.Amount,
		Currency:    order.Currency,
		NotifyURL:   s.notifyURLForProvider(provider),
	}

	switch provider {
	case models.PaymentProviderWeChat:
		if s.wechat != nil && s.wechat.Enabled() {
			result, err := s.wechat.CreateNativeOrder(ctx, createReq)
			if err != nil {
				return order, product, nil, err
			}
			if err := s.applyCreateResult(order, result); err != nil {
				return nil, nil, nil, err
			}
			checkout = &PaymentCheckout{
				Provider:        provider,
				PaymentEnabled:  true,
				CodeURL:         result.CodeURL,
				ProviderOrderID: result.ProviderOrderID,
				ProviderTradeNo: result.ProviderTradeNo,
				Message:         "wechat native order created",
			}
		}
	case models.PaymentProviderAlipay:
		if s.alipay != nil && s.alipay.Enabled() {
			result, err := s.alipay.CreatePrecreateOrder(ctx, createReq)
			if err != nil {
				return order, product, nil, err
			}
			if err := s.applyCreateResult(order, result); err != nil {
				return nil, nil, nil, err
			}
			checkout = &PaymentCheckout{
				Provider:        provider,
				PaymentEnabled:  true,
				CodeURL:         result.CodeURL,
				ProviderOrderID: result.ProviderOrderID,
				ProviderTradeNo: result.ProviderTradeNo,
				Message:         "alipay precreate order created",
			}
		}
	}

	return order, product, checkout, nil
}

func (s *PaymentService) RefreshOrderStatus(ctx context.Context, order *models.PaymentOrder) (*models.PaymentOrder, error) {
	if order == nil {
		return nil, fmt.Errorf("payment order is nil")
	}
	if order.Status == models.PaymentOrderStatusPaid || order.Status == models.PaymentOrderStatusClosed || order.Status == models.PaymentOrderStatusRefunded {
		if order.Status == models.PaymentOrderStatusPaid {
			if err := s.grantPlanIfPaid(order); err != nil {
				return nil, err
			}
		}
		return order, nil
	}
	var result *payments.QueryOrderResult
	var err error
	switch order.PaymentProvider {
	case models.PaymentProviderWeChat:
		if s.wechat == nil || !s.wechat.Enabled() {
			return order, nil
		}
		result, err = s.wechat.QueryOrder(ctx, order.OrderNo)
	case models.PaymentProviderAlipay:
		if s.alipay == nil || !s.alipay.Enabled() {
			return order, nil
		}
		result, err = s.alipay.QueryOrder(ctx, order.OrderNo)
	default:
		return order, nil
	}
	if err != nil {
		return nil, err
	}
	if err := s.applyQueryResult(order, result); err != nil {
		return nil, err
	}
	return order, nil
}

func (s *PaymentService) HandleWeChatNotify(headers map[string]string, body []byte) (*models.PaymentOrder, error) {
	if s.wechat == nil || !s.wechat.Enabled() {
		return nil, fmt.Errorf("wechat payment is not enabled")
	}
	httpHeaders := make(map[string][]string)
	for key, value := range headers {
		httpHeaders[key] = []string{value}
	}
	result, err := s.wechat.ParseNotify(http.Header(httpHeaders), body)
	if err != nil {
		return nil, err
	}
	return s.applyNotifyResult(models.PaymentProviderWeChat, result)
}

func (s *PaymentService) HandleAlipayNotify(values map[string]string) (*models.PaymentOrder, error) {
	if s.alipay == nil || !s.alipay.Enabled() {
		return nil, fmt.Errorf("alipay payment is not enabled")
	}
	form := make(map[string][]string)
	for key, value := range values {
		form[key] = []string{value}
	}
	result, err := s.alipay.ParseNotify(url.Values(form))
	if err != nil {
		return nil, err
	}
	return s.applyNotifyResult(models.PaymentProviderAlipay, result)
}

func (s *PaymentService) UpsertProduct(productCode string, input PaymentProductInput) (*models.PaymentProduct, error) {
	productCode = normalizeProductCode(productCode)
	if productCode == "" {
		return nil, fmt.Errorf("product_code is required")
	}
	input.ProductCode = productCode
	if err := validatePaymentProductInput(input); err != nil {
		return nil, err
	}
	var existing models.PaymentProduct
	err := s.db.Where("product_code = ?", productCode).First(&existing).Error
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	if err == gorm.ErrRecordNotFound {
		product := models.PaymentProduct{
			ProductCode:  input.ProductCode,
			Name:         strings.TrimSpace(input.Name),
			DisplayName:  strings.TrimSpace(input.DisplayName),
			PlanCode:     strings.TrimSpace(input.PlanCode),
			PriceAmount:  input.PriceAmount,
			Currency:     normalizeCurrency(input.Currency),
			DurationDays: input.DurationDays,
			Status:       normalizeProductStatus(input.Status),
			SortOrder:    input.SortOrder,
			Description:  strings.TrimSpace(input.Description),
		}
		if err := s.db.Create(&product).Error; err != nil {
			return nil, err
		}
		return &product, nil
	}
	existing.Name = strings.TrimSpace(input.Name)
	existing.DisplayName = strings.TrimSpace(input.DisplayName)
	existing.PlanCode = strings.TrimSpace(input.PlanCode)
	existing.PriceAmount = input.PriceAmount
	existing.Currency = normalizeCurrency(input.Currency)
	existing.DurationDays = input.DurationDays
	existing.Status = normalizeProductStatus(input.Status)
	existing.SortOrder = input.SortOrder
	existing.Description = strings.TrimSpace(input.Description)
	if err := s.db.Save(&existing).Error; err != nil {
		return nil, err
	}
	return &existing, nil
}

func (s *PaymentService) applyCreateResult(order *models.PaymentOrder, result *payments.CreateOrderResult) error {
	if order == nil || result == nil {
		return nil
	}
	if result.ProviderOrderID != "" {
		order.ProviderOrderID = &result.ProviderOrderID
	}
	if result.ProviderTradeNo != "" {
		order.ProviderTradeNo = &result.ProviderTradeNo
	}
	if len(result.Raw) > 0 {
		order.ProviderPayload = datatypes.JSON(result.Raw)
	}
	return s.db.Save(order).Error
}

func (s *PaymentService) applyQueryResult(order *models.PaymentOrder, result *payments.QueryOrderResult) error {
	if order == nil || result == nil {
		return nil
	}
	wasPaid := order.Status == models.PaymentOrderStatusPaid
	if result.ProviderOrderID != "" {
		order.ProviderOrderID = &result.ProviderOrderID
	}
	if result.ProviderTradeNo != "" {
		order.ProviderTradeNo = &result.ProviderTradeNo
	}
	if len(result.Raw) > 0 {
		order.ProviderPayload = datatypes.JSON(result.Raw)
	}
	order.Status = models.PaymentOrderStatus(result.Status)
	if result.PaidAt != nil {
		order.PaidAt = result.PaidAt
	}
	if err := s.db.Save(order).Error; err != nil {
		return err
	}
	if !wasPaid && order.Status == models.PaymentOrderStatusPaid {
		return s.grantPlanIfPaid(order)
	}
	return nil
}

func (s *PaymentService) applyNotifyResult(provider models.PaymentProvider, result *payments.NotifyResult) (*models.PaymentOrder, error) {
	order, err := s.FindOrderByNo(result.OrderNo)
	if err != nil {
		return nil, err
	}
	wasPaid := order.Status == models.PaymentOrderStatusPaid
	if result.ProviderOrderID != "" {
		order.ProviderOrderID = &result.ProviderOrderID
	}
	if result.ProviderTradeNo != "" {
		order.ProviderTradeNo = &result.ProviderTradeNo
	}
	if result.PaidAt != nil {
		order.PaidAt = result.PaidAt
	}
	order.Status = models.PaymentOrderStatus(result.Status)
	if len(result.Raw) > 0 {
		order.ProviderPayload = datatypes.JSON(result.Raw)
	}
	if err := s.db.Save(order).Error; err != nil {
		return nil, err
	}
	callbackPayload := datatypes.JSON(result.Raw)
	callback := models.PaymentCallback{
		Provider:        provider,
		OrderNo:         order.OrderNo,
		ProviderTradeNo: derefString(order.ProviderTradeNo),
		Payload:         callbackPayload,
		Status:          string(order.Status),
	}
	if err := s.db.Create(&callback).Error; err != nil {
		return nil, err
	}
	if !wasPaid && order.Status == models.PaymentOrderStatusPaid {
		if err := s.grantPlanIfPaid(order); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func (s *PaymentService) grantPlanIfPaid(order *models.PaymentOrder) error {
	if order == nil || order.Status != models.PaymentOrderStatusPaid {
		return nil
	}
	if s.plan == nil {
		return fmt.Errorf("plan service is not configured")
	}

	var product models.PaymentProduct
	if err := s.db.First(&product, order.ProductID).Error; err != nil {
		return err
	}

	if sub, plan, err := s.plan.GetActiveSubscription(order.UserID); err == nil && sub != nil && plan != nil {
		if strings.EqualFold(plan.PlanCode, product.PlanCode) {
			return nil
		}
	}

	var expiresAt *time.Time
	if product.DurationDays > 0 {
		next := time.Now().Add(time.Duration(product.DurationDays) * 24 * time.Hour)
		expiresAt = &next
	}

	return s.plan.AssignPlan(order.UserID, product.PlanCode, expiresAt)
}

func (s *PaymentService) notifyURLForProvider(provider models.PaymentProvider) string {
	switch provider {
	case models.PaymentProviderWeChat:
		return strings.TrimSpace(s.cfg.WeChat.NotifyURL)
	case models.PaymentProviderAlipay:
		return strings.TrimSpace(s.cfg.Alipay.NotifyURL)
	default:
		return ""
	}
}

func normalizeProductCode(value string) string {
	return strings.TrimSpace(strings.ToLower(value))
}

func normalizeCurrency(value string) string {
	trimmed := strings.TrimSpace(strings.ToUpper(value))
	if trimmed == "" {
		return "CNY"
	}
	return trimmed
}

func normalizeProductStatus(status models.PaymentProductStatus) models.PaymentProductStatus {
	if status == "" {
		return models.PaymentProductStatusActive
	}
	return status
}

func validatePaymentProductInput(input PaymentProductInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(input.DisplayName) == "" {
		return fmt.Errorf("display_name is required")
	}
	if strings.TrimSpace(input.PlanCode) == "" {
		return fmt.Errorf("plan_code is required")
	}
	if input.PriceAmount < 0 {
		return fmt.Errorf("price_amount must be >= 0")
	}
	if input.DurationDays <= 0 {
		return fmt.Errorf("duration_days must be > 0")
	}
	if input.Status != "" && input.Status != models.PaymentProductStatusActive && input.Status != models.PaymentProductStatusDisabled {
		return fmt.Errorf("invalid product status")
	}
	return nil
}

func newOrderNo() string {
	now := time.Now()
	return "CP" + now.Format("20060102150405") + strconv.FormatInt(now.UnixNano()%1_000_000, 10)
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
