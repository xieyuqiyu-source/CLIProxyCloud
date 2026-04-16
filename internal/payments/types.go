package payments

import "time"

type CreateOrderRequest struct {
	OrderNo     string
	Description string
	Amount      int64
	Currency    string
	NotifyURL   string
	ReturnURL   string
	CallbackURL string
	ExpiresIn   time.Duration
}

type CreateOrderResult struct {
	ProviderOrderID string
	ProviderTradeNo string
	CodeURL         string
	Raw             []byte
}

type QueryOrderResult struct {
	ProviderOrderID string
	ProviderTradeNo string
	Status          string
	PaidAt          *time.Time
	Raw             []byte
}

type NotifyResult struct {
	OrderNo         string
	ProviderOrderID string
	ProviderTradeNo string
	Status          string
	PaidAt          *time.Time
	Raw             []byte
}
