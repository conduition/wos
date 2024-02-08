package wos

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Addresses represents the on-chain and lightning deposit addresses for
// a [Wallet].
type Addresses struct {
	OnChain   string `json:"btcDepositAddress"`
	Lightning string `json:"lightningAddress"`
}

// Balance represents a [Wallet]'s balance at a certain point in time.
type Balance struct {
	Confirmed   float64 `json:"btc"`
	Unconfirmed float64 `json:"btcUnconfirmed"`

	// This field is unreliable. It can even go negative in some cases.
	// The `btc` field is the real confirmed balance.
	// Lightning float64 `json:"lightning"`
}

// Total returns the sum of the confirmed and unconfirmed balances.
func (b Balance) Total() float64 {
	return b.Confirmed + b.Unconfirmed
}

type FeeEstimate struct {
	BtcFixedFee              float64 `json:"btcFixedFee"`
	BtcMinerFeePerKB         float64 `json:"btcMinerFeePerKb"`
	BtcSendCommissionPercent float64 `json:"btcSendCommissionPercent"`
	BtcSendFeeWarningPercent float64 `json:"btcSendFeeWarningPercent"`
	LightningFee             float64 `json:"lightningFee"`
	MaxLightningFee          float64 `json:"sendMaxLightningFee"`
	IsWosInvoice             bool    `json:"wosInvoice"`
}

type (
	// PaymentStatus represents the status of a [Payment].
	PaymentStatus string

	// PaymentType indicates whether a payment was incoming or outgoing from a wallet.
	PaymentType string

	// PaymentCurrency indicates whether a payment was made on-chain or via Lightning.
	PaymentCurrency string
)

const (
	PaymentStatusPaid    PaymentStatus = "PAID"    // The payment has been completed and confirmed.
	PaymentStatusPending PaymentStatus = "PENDING" // An on-chain payment is still confirming.

	PaymentTypeCredit PaymentType = "CREDIT" // A received payment.
	PaymentTypeDebit  PaymentType = "DEBIT"  // A sent payment.

	PaymentCurrencyBitcoin   PaymentCurrency = "BTC"       // On-chain bitcoin.
	PaymentCurrencyLightning PaymentCurrency = "LIGHTNING" // Off-chain lightning network credit.
)

// Payment represents an on-chain or lightning payment, either received or sent.
type Payment struct {
	// A UUID identifying the payment.
	ID string `json:"id"`

	// For on-chain bitcoin, this is the address the payment was sent to.
	// For lightning, this is the invoice or LN address.
	Address string `json:"address"`

	// Amount is the Bitcoin-denominated amount of the payment.
	Amount float64 `json:"amount"`

	// Currency is either PaymentCurrencyBitcoin or PaymentCurrencyLightning.
	Currency PaymentCurrency `json:"currency"`

	// Invoice description, or empty string otherwise.
	Description string `json:"description"`

	// Invoice expiry time. Empty for debits.
	Expires time.Time `json:"expires"`

	// If WoS thinks this payment is spam.
	IsLikelySpam bool `json:"isLikelySpam"`

	// If the payment came from the WOS point-of-sale system.
	IsPointOfSale bool `json:"isWosPos"`

	// Time the payment occurred.
	Time time.Time `json:"time"`

	// For lightning payments, this is the payment hash.
	Txid string `json:"transactionId"`

	// Status is either PaymentStatusPaid or PaymentStatusPending.
	// More statuses may exist.
	Status PaymentStatus `json:"status"`

	// Type is either PaymentTypeCredit or PaymentTypeDebit.
	Type PaymentType `json:"type"`
}

// Reader facilitates read-only access to a WoS wallet.
// It can be used to fetch balances, payment history,
// and estimate fees.
type Reader struct {
	apiToken   string
	httpClient *http.Client
}

// NewReader constructs a Reader from a given [http.Client] and read-only apiToken.
//
// Uses [http.DefaultClient] if httpClient is nil.
func NewReader(apiToken string, httpClient *http.Client) *Reader {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Reader{apiToken, httpClient}
}

// GetRequest issues a GET request to the given endpoint, authenticated with
// the Reader's API token.
func (rdr *Reader) GetRequest(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", BaseURL+endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "")
	req.Header.Set("Api-Token", rdr.apiToken)

	resp, err := rdr.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s request failed: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if err := checkHTTPResponse(resp); err != nil {
		return nil, fmt.Errorf("GET %s: %w", endpoint, err)
	}

	return io.ReadAll(resp.Body)
}

// Addresses re-fetches the wallet's on-chain and lightning addresses.
// This can be useful to ensure you have the wallet's latest unused
// on-chain deposit address.
func (rdr *Reader) Addresses(ctx context.Context) (*Addresses, error) {
	respData, err := rdr.GetRequest(ctx, "/api/v1/wallet/account")
	if err != nil {
		return nil, fmt.Errorf("Addresses: %w", err)
	}

	var addresses Addresses
	if err := json.Unmarshal(respData, &addresses); err != nil {
		return nil, fmt.Errorf("error decoding Addresses response: %w", err)
	}

	if addresses.OnChain == "" {
		return nil, fmt.Errorf("Addresses: unsupported region")
	}
	return &addresses, nil
}

// Balance returns the current confirmed and unconfirmed balances of the wallet.
func (rdr *Reader) Balance(ctx context.Context) (*Balance, error) {
	respData, err := rdr.GetRequest(ctx, "/api/v1/wallet/balance")
	if err != nil {
		return nil, fmt.Errorf("Balance: %w", err)
	}

	balance := new(Balance)
	if err := json.Unmarshal(respData, balance); err != nil {
		return nil, fmt.Errorf("invalid Balance response: %w", err)
	}

	return balance, nil
}

// FeeEstimate fetches the latest fee estimation data when paying to a given on-chain
// address or lightning invoice.
func (rdr *Reader) FeeEstimate(ctx context.Context, addressOrInvoice string) (*FeeEstimate, error) {
	query := make(url.Values)
	if addressOrInvoice != "" {
		query.Set("address", addressOrInvoice)
	}
	if amt, err := parseInvoiceAmount(addressOrInvoice); err == nil {
		query.Set("amount", strconv.FormatFloat(amt, 'f', 11, 64))
	}

	respData, err := rdr.GetRequest(ctx, "/api/v1/wallet/feeEstimate?"+query.Encode())
	if err != nil {
		return nil, fmt.Errorf("FeeEstimate: %w", err)
	}

	var estimate FeeEstimate
	if err := json.Unmarshal(respData, &estimate); err != nil {
		return nil, fmt.Errorf("invalid FeeEstimate response: %w", err)
	}
	return &estimate, nil
}

func (rdr *Reader) BalanceAndFee(
	ctx context.Context,
	addressOrInvoice string,
) (*Balance, *FeeEstimate, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	balanceChan := make(chan *Balance)
	feeChan := make(chan *FeeEstimate)
	errChan := make(chan error)

	go func() {
		balance, err := rdr.Balance(ctx)
		if err != nil {
			select {
			case errChan <- err:
			case <-ctx.Done():
			}
		}

		select {
		case balanceChan <- balance:
		case <-ctx.Done():
		}
	}()

	go func() {
		fees, err := rdr.FeeEstimate(ctx, addressOrInvoice)
		if err != nil {
			select {
			case errChan <- err:
			case <-ctx.Done():
			}
		}

		select {
		case feeChan <- fees:
		case <-ctx.Done():
		}
	}()

	var balance *Balance
	var fees *FeeEstimate

	for i := 0; i < 2; i++ {
		select {
		case balance = <-balanceChan:
		case fees = <-feeChan:
		case err := <-errChan:
			return balance, fees, err
		}
	}

	return balance, fees, nil
}

func (rdr *Reader) ListPayments(ctx context.Context) ([]Payment, error) {
	query := make(url.Values)

	query.Set("skip", "0")
	// query.Set("limit", "99999")
	query.Set("reverse", "false") // ascending
	// query.Set("reverse", "true") // descending

	respData, err := rdr.GetRequest(ctx, "/api/v1/wallet/payment?"+query.Encode())
	if err != nil {
		return nil, fmt.Errorf("ListPayments: %w", err)
	}

	var payments []Payment
	if err := json.Unmarshal(respData, &payments); err != nil {
		return nil, fmt.Errorf("invalid ListPayments response: %w", err)
	}

	return payments, nil
}
