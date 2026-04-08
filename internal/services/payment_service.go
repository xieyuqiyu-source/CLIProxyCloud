package services

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xieyuqiyu-source/CLIProxyCloud/internal/models"
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

type PaymentService struct {
	db *gorm.DB
}

func NewPaymentService(db *gorm.DB) *PaymentService {
	return &PaymentService{db: db}
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
	err := s.db.
		Where("status = ?", models.PaymentProductStatusActive).
		Order("sort_order asc, id asc").
		Find(&products).Error
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

func (s *PaymentService) CreateOrder(userID uint, productCode string, provider models.PaymentProvider) (*models.PaymentOrder, *models.PaymentProduct, error) {
	if provider != models.PaymentProviderWeChat && provider != models.PaymentProviderAlipay {
		return nil, nil, fmt.Errorf("unsupported payment provider")
	}

	product, err := s.FindProductByCode(productCode)
	if err != nil {
		return nil, nil, err
	}
	if product.Status != models.PaymentProductStatusActive {
		return nil, nil, fmt.Errorf("payment product is disabled")
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
		return nil, nil, err
	}
	return order, product, nil
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
	if input.Status != "" &&
		input.Status != models.PaymentProductStatusActive &&
		input.Status != models.PaymentProductStatusDisabled {
		return fmt.Errorf("invalid product status")
	}
	return nil
}

func newOrderNo() string {
	return "cpo" + strconv.FormatInt(time.Now().UnixNano(), 10)
}
