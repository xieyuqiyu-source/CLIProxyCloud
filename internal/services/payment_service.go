package services

import (
	"context"
	"encoding/json"
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
	"gorm.io/gorm/clause"
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

type PaymentQuote struct {
	ProductCode        string                     `json:"productCode"`
	ProductDisplayName string                     `json:"productDisplayName"`
	PlanCode           string                     `json:"planCode"`
	PurchaseMode       models.PaymentPurchaseMode `json:"purchaseMode"`
	BillingMonths      int                        `json:"billingMonths"`
	Amount             int64                      `json:"amount"`
	Currency           string                     `json:"currency"`
	DurationDays       int                        `json:"durationDays"`
	Title              string                     `json:"title"`
	Description        string                     `json:"description"`
	Allowed            bool                       `json:"allowed"`
	Reason             string                     `json:"reason,omitempty"`
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
		productCode := normalizeProductCode(product.ProductCode)
		var existing models.PaymentProduct
		err := s.db.Where("product_code = ?", productCode).First(&existing).Error
		if err == nil {
			continue
		}
		if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		if _, err := s.UpsertProduct(productCode, product); err != nil {
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

func (s *PaymentService) QuoteOrder(userID uint, productCode string, billingMonths int, purchaseMode models.PaymentPurchaseMode) (*models.PaymentProduct, *PaymentQuote, error) {
	product, err := s.FindProductByCode(productCode)
	if err != nil {
		return nil, nil, err
	}
	if product.Status != models.PaymentProductStatusActive {
		return nil, nil, fmt.Errorf("payment product is disabled")
	}

	purchaseMode = normalizePurchaseMode(purchaseMode)
	billingMonths = normalizeBillingMonths(billingMonths)

	var (
		activeSub  *models.UserSubscription
		activePlan *models.Plan
	)
	if s.plan != nil {
		if sub, plan, err := s.plan.GetActiveSubscription(userID); err == nil {
			activeSub = sub
			activePlan = plan
		} else if err != gorm.ErrRecordNotFound {
			return nil, nil, err
		}
	}

	currentPlanCode := "free"
	if activePlan != nil {
		currentPlanCode = strings.TrimSpace(strings.ToLower(activePlan.PlanCode))
	}
	targetPlanCode := strings.TrimSpace(strings.ToLower(product.PlanCode))
	quote := &PaymentQuote{
		ProductCode:        product.ProductCode,
		ProductDisplayName: product.DisplayName,
		PlanCode:           product.PlanCode,
		PurchaseMode:       purchaseMode,
		BillingMonths:      billingMonths,
		Currency:           product.Currency,
		Allowed:            true,
	}

	switch purchaseMode {
	case models.PaymentPurchaseModeUpgradeDiffAll:
		if currentPlanCode != "vip1" || targetPlanCode != "vip2" {
			return nil, nil, fmt.Errorf("upgrade diff mode is only available when upgrading Pro to Pro Max")
		}
		if activeSub == nil || activeSub.ExpiresAt == nil || !activeSub.ExpiresAt.After(time.Now()) {
			return nil, nil, fmt.Errorf("current Pro subscription is not active")
		}
		baseProduct, err := s.FindProductByCode("pro_monthly")
		if err != nil {
			return nil, nil, err
		}
		targetMonthly, err := s.FindProductByCode("pro_max_monthly")
		if err != nil {
			return nil, nil, err
		}
		remainingMonths := remainingBillingMonths(time.Now(), *activeSub.ExpiresAt)
		if remainingMonths <= 0 {
			return nil, nil, fmt.Errorf("remaining Pro duration is less than one billable month")
		}
		diffAmount := targetMonthly.PriceAmount - baseProduct.PriceAmount
		if diffAmount <= 0 {
			return nil, nil, fmt.Errorf("Pro Max monthly price must be higher than Pro monthly price")
		}
		quote.PurchaseMode = models.PaymentPurchaseModeUpgradeDiffAll
		quote.BillingMonths = remainingMonths
		quote.Amount = diffAmount * int64(remainingMonths)
		quote.DurationDays = 0
		quote.Title = fmt.Sprintf("Pro Max 补差价升级（%d 个月）", remainingMonths)
		quote.Description = fmt.Sprintf("按剩余 %d 个月补差价，升级成功后将当前 Pro 直接升级为 Pro Max，到期时间保持不变。", remainingMonths)
		return product, quote, nil
	case models.PaymentPurchaseModeUpgradeReplaceMonth:
		if currentPlanCode != "vip1" || targetPlanCode != "vip2" {
			return nil, nil, fmt.Errorf("replacement upgrade is only available when upgrading Pro to Pro Max")
		}
		quote.PurchaseMode = models.PaymentPurchaseModeUpgradeReplaceMonth
		quote.BillingMonths = 1
		quote.Amount = product.PriceAmount
		quote.DurationDays = 30
		quote.Title = "Pro Max 重新开通 1 个月"
		quote.Description = "当前 Pro 剩余时长将失效，并从支付成功时起重新开通 1 个月 Pro Max。"
		return product, quote, nil
	default:
		if currentPlanCode == "vip2" && targetPlanCode == "vip1" {
			return nil, nil, fmt.Errorf("Pro Max cannot be downgraded to Pro")
		}
		if currentPlanCode == "vip1" && targetPlanCode == "vip2" {
			return nil, nil, fmt.Errorf("please choose an upgrade mode to switch from Pro to Pro Max")
		}
		quote.PurchaseMode = models.PaymentPurchaseModeStandard
		quote.Amount = discountedAmount(product.PriceAmount, billingMonths)
		quote.DurationDays = billingMonths * 30
		quote.Title = fmt.Sprintf("%s%s", product.DisplayName, billingLabelSuffix(billingMonths))
		quote.Description = discountedDescription(product.Description, billingMonths)
		return product, quote, nil
	}
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

func (s *PaymentService) ListAdminOrders(limit int, status string, query string) ([]models.PaymentOrder, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var orders []models.PaymentOrder
	db := s.db.Model(&models.PaymentOrder{})
	status = strings.TrimSpace(strings.ToLower(status))
	if status != "" && status != "all" {
		db = db.Where("status = ?", status)
	}
	query = strings.TrimSpace(query)
	if query != "" {
		like := "%" + query + "%"
		db = db.Where(
			"order_no LIKE ? OR plan_code LIKE ? OR product_code LIKE ? OR product_display LIKE ? OR payment_provider LIKE ?",
			like, like, like, like, like,
		)
		if userID, err := strconv.ParseUint(query, 10, 64); err == nil {
			db = db.Or("user_id = ?", userID)
		}
	}
	err := db.Order("id desc").Limit(limit).Find(&orders).Error
	return orders, err
}

func (s *PaymentService) CreateOrder(ctx context.Context, userID uint, productCode string, provider models.PaymentProvider, billingMonths int, purchaseMode models.PaymentPurchaseMode) (*models.PaymentOrder, *models.PaymentProduct, *PaymentCheckout, error) {
	if provider != models.PaymentProviderWeChat && provider != models.PaymentProviderAlipay {
		return nil, nil, nil, fmt.Errorf("unsupported payment provider")
	}
	product, quote, err := s.QuoteOrder(userID, productCode, billingMonths, purchaseMode)
	if err != nil {
		return nil, nil, nil, err
	}

	if existing, checkout, err := s.findReusablePendingOrder(userID, product, provider, quote); err != nil {
		return nil, nil, nil, err
	} else if existing != nil {
		return existing, product, checkout, nil
	}

	expiresAt := time.Now().Add(5 * time.Minute)
	order := &models.PaymentOrder{
		OrderNo:         newOrderNo(),
		UserID:          userID,
		ProductID:       product.ID,
		ProductCode:     product.ProductCode,
		ProductName:     product.Name,
		ProductDisplay:  quote.Title,
		ProductDesc:     quote.Description,
		PurchaseMode:    quote.PurchaseMode,
		BillingMonths:   quote.BillingMonths,
		DurationDays:    quote.DurationDays,
		PlanCode:        product.PlanCode,
		PaymentProvider: provider,
		Amount:          quote.Amount,
		Currency:        quote.Currency,
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
		Description: quote.Title,
		Amount:      order.Amount,
		Currency:    order.Currency,
		NotifyURL:   s.notifyURLForProvider(provider),
		ExpiresIn:   5 * time.Minute,
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

func (s *PaymentService) findReusablePendingOrder(userID uint, product *models.PaymentProduct, provider models.PaymentProvider, quote *PaymentQuote) (*models.PaymentOrder, *PaymentCheckout, error) {
	if product == nil {
		return nil, nil, nil
	}

	now := time.Now()
	var order models.PaymentOrder
	err := s.db.
		Where("user_id = ? AND product_id = ? AND payment_provider = ? AND status = ? AND purchase_mode = ? AND billing_months = ?", userID, product.ID, provider, models.PaymentOrderStatusPending, quote.PurchaseMode, quote.BillingMonths).
		Where("expires_at IS NULL OR expires_at > ?", now).
		Order("id desc").
		First(&order).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}

	checkout := s.checkoutFromExistingOrder(&order)
	checkout.Message = "reused pending payment order"
	return &order, checkout, nil
}

func (s *PaymentService) RefreshOrderStatus(ctx context.Context, order *models.PaymentOrder) (*models.PaymentOrder, error) {
	if order == nil {
		return nil, fmt.Errorf("payment order is nil")
	}
	if order.Status == models.PaymentOrderStatusPaid || order.Status == models.PaymentOrderStatusClosed || order.Status == models.PaymentOrderStatusRefunded {
		if order.Status == models.PaymentOrderStatusPaid && order.GrantedAt == nil {
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
	return s.grantPlan(order, false)
}

func (s *PaymentService) grantPlan(order *models.PaymentOrder, force bool) error {
	if order == nil {
		return nil
	}
	if s.plan == nil {
		return fmt.Errorf("plan service is not configured")
	}

	return s.db.Transaction(func(tx *gorm.DB) error {
		var locked models.PaymentOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, order.ID).Error; err != nil {
			return err
		}
		if locked.Status != models.PaymentOrderStatusPaid {
			return nil
		}
		if locked.GrantedAt != nil && !force {
			return nil
		}

		planSvc := NewPlanService(tx)
		now := time.Now()
		baseTime := now
		var (
			currentSub  *models.UserSubscription
			currentPlan *models.Plan
		)
		if sub, plan, err := planSvc.GetActiveSubscription(locked.UserID); err == nil && sub != nil && plan != nil {
			currentSub = sub
			currentPlan = plan
			if strings.EqualFold(plan.PlanCode, locked.PlanCode) && sub.ExpiresAt != nil && sub.ExpiresAt.After(baseTime) {
				baseTime = *sub.ExpiresAt
			}
		}

		var expiresAt *time.Time
		switch locked.PurchaseMode {
		case models.PaymentPurchaseModeUpgradeDiffAll:
			if currentSub == nil || currentPlan == nil || !strings.EqualFold(currentPlan.PlanCode, "vip1") || locked.PlanCode != "vip2" {
				return fmt.Errorf("upgrade diff grant requires an active Pro subscription")
			}
			if currentSub.ExpiresAt != nil && currentSub.ExpiresAt.After(now) {
				next := *currentSub.ExpiresAt
				expiresAt = &next
			}
		case models.PaymentPurchaseModeUpgradeReplaceMonth:
			next := addPlanDuration(now, 30)
			expiresAt = &next
		default:
			if locked.DurationDays > 0 {
				next := addPlanDuration(baseTime, locked.DurationDays)
				expiresAt = &next
			}
		}

		if err := planSvc.AssignPlan(locked.UserID, locked.PlanCode, expiresAt); err != nil {
			return err
		}
		nowGranted := time.Now()
		if err := tx.Model(&locked).Update("granted_at", &nowGranted).Error; err != nil {
			return err
		}
		return nil
	})
}

func (s *PaymentService) RegrantOrder(orderNo string) (*models.PaymentOrder, error) {
	order, err := s.FindOrderByNo(orderNo)
	if err != nil {
		return nil, err
	}
	if order.Status != models.PaymentOrderStatusPaid {
		return nil, fmt.Errorf("only paid orders can be regranted")
	}
	if err := s.grantPlan(order, true); err != nil {
		return nil, err
	}
	return order, nil
}

func (s *PaymentService) checkoutFromExistingOrder(order *models.PaymentOrder) *PaymentCheckout {
	checkout := &PaymentCheckout{
		Provider:       order.PaymentProvider,
		PaymentEnabled: true,
	}
	if order.ProviderOrderID != nil {
		checkout.ProviderOrderID = derefString(order.ProviderOrderID)
	}
	if order.ProviderTradeNo != nil {
		checkout.ProviderTradeNo = derefString(order.ProviderTradeNo)
	}
	if len(order.ProviderPayload) == 0 {
		return checkout
	}

	var payload map[string]any
	if err := json.Unmarshal(order.ProviderPayload, &payload); err != nil {
		return checkout
	}
	switch order.PaymentProvider {
	case models.PaymentProviderWeChat:
		if codeURL, ok := payload["code_url"].(string); ok {
			checkout.CodeURL = strings.TrimSpace(codeURL)
		}
	case models.PaymentProviderAlipay:
		if nested, ok := payload["alipay_trade_precreate_response"].(map[string]any); ok {
			if codeURL, ok := nested["qr_code"].(string); ok {
				checkout.CodeURL = strings.TrimSpace(codeURL)
			}
			if tradeNo, ok := nested["trade_no"].(string); ok {
				checkout.ProviderTradeNo = strings.TrimSpace(tradeNo)
			}
		}
	}
	return checkout
}

func addPlanDuration(start time.Time, durationDays int) time.Time {
	if durationDays <= 0 {
		return start
	}
	if durationDays%30 == 0 {
		return start.AddDate(0, durationDays/30, 0)
	}
	return start.Add(time.Duration(durationDays) * 24 * time.Hour)
}

func discountedAmount(monthlyAmount int64, billingMonths int) int64 {
	switch billingMonths {
	case 6:
		return int64(float64(monthlyAmount*6) * 0.9)
	case 12:
		return int64(float64(monthlyAmount*12) * 0.7)
	default:
		return monthlyAmount
	}
}

func normalizeBillingMonths(value int) int {
	switch value {
	case 6, 12:
		return value
	default:
		return 1
	}
}

func normalizePurchaseMode(value models.PaymentPurchaseMode) models.PaymentPurchaseMode {
	switch value {
	case models.PaymentPurchaseModeUpgradeDiffAll, models.PaymentPurchaseModeUpgradeReplaceMonth:
		return value
	default:
		return models.PaymentPurchaseModeStandard
	}
}

func billingLabelSuffix(months int) string {
	switch months {
	case 6:
		return "（半年付）"
	case 12:
		return "（年付）"
	default:
		return "（月付）"
	}
}

func discountedDescription(base string, months int) string {
	switch months {
	case 6:
		return strings.TrimSpace(base + " 半年付享 9 折。")
	case 12:
		return strings.TrimSpace(base + " 年付享 7 折。")
	default:
		return base
	}
}

func remainingBillingMonths(now time.Time, expiresAt time.Time) int {
	if !expiresAt.After(now) {
		return 0
	}
	count := 0
	cursor := now
	for cursor.Before(expiresAt) {
		count++
		cursor = cursor.AddDate(0, 1, 0)
	}
	return count
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
