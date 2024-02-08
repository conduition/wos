package wos

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// BaseURL is the API URL for the Wallet of Satoshi API.
const BaseURL = "https://www.livingroomofsatoshi.com"

type errorResponse struct {
	Message string
}

func checkHTTPResponse(resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}

	err := fmt.Errorf("received status %d", resp.StatusCode)

	rawBody, readErr := io.ReadAll(resp.Body)
	if readErr == nil {

		var respErrDetail errorResponse
		decodeErr := json.Unmarshal(rawBody, &respErrDetail)
		if decodeErr == nil && respErrDetail.Message != "" {
			err = fmt.Errorf("%w: %s", err, respErrDetail.Message)
		} else {
			err = fmt.Errorf("%w: %s", err, string(rawBody))
		}

	} else {
		err = fmt.Errorf("%w: (failed to read body: %w)", err, readErr)
	}

	return err
}

// Credentials represents a full set of credentials for a WoS wallet.
type Credentials struct {
	// APISecret is a base58 secret needed for write-access to a wallet.
	// This secret is used to sign any requests to POST endpoints, such
	// as those which create invoices and make payments.
	//
	// APISecret is useless on its own, but if the corresponding APIToken is
	// available, it permits full access to spend funds from a WoS wallet.
	APISecret string `json:"apiSecret"`

	// APIToken is a read-only access token used to fetch balance information,
	// estimate fees, and view the transaction history of a wallet.
	//
	// Without the APISecret, it can only be used to view a WoS wallet, but
	// it cannot modify the wallet's state in any way.
	APIToken string `json:"apiToken"`
}

// Reader builds Generate a [Reader] object from the APIToken.
//
// HTTP API calls made by the reader will be executed by the given [http.Client].
func (creds Credentials) Reader(httpClient *http.Client) *Reader {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return NewReader(creds.APIToken, httpClient)
}

// SimpleSigner returns a [SimpleSigner] which signs using the APISecret.
func (creds Credentials) SimpleSigner() *SimpleSigner {
	return NewSimpleSigner(creds.APISecret)
}

// OpenWallet opens a [Wallet] using the given [http.Client] for all API calls.
func (creds Credentials) OpenWallet(
	ctx context.Context,
	httpClient *http.Client,
) (*Wallet, error) {
	return OpenWallet(ctx, creds.Reader(httpClient), creds.SimpleSigner())
}

// Wallet represents a Wallet of Satoshi wallet, including the mechanisms
// needed to read its history and balances, create invoices, and make payments.
//
// To create a brand new wallet from scratch, use [CreateWallet].
//
// To open an existing wallet from a set of [Credentials], use [Credentials.OpenWallet].
//
// To open a wallet from an isolated signing mechanism, use [OpenWallet] with a
// given [Signer].
type Wallet struct {
	reader           *Reader
	signer           Signer
	httpClient       *http.Client
	onChainAddress   string
	lightningAddress string
}

// OpenWallet opens an existing wallet using a separate [Reader] and [Signer].
//
// The [Reader] will be used to fetch read-only information about the wallet, while
// the [Signer] authenticates write calls.
func OpenWallet(ctx context.Context, reader *Reader, signer Signer) (*Wallet, error) {
	addresses, err := reader.Addresses(ctx)
	if err != nil {
		return nil, fmt.Errorf("OpenWallet: %w", err)
	}

	wallet := &Wallet{
		reader:           reader,
		signer:           signer,
		httpClient:       reader.httpClient,
		onChainAddress:   addresses.OnChain,
		lightningAddress: addresses.Lightning,
	}

	return wallet, nil
}

type createWalletResponse struct {
	APISecret        string `json:"apiSecret"`
	APIToken         string `json:"apiToken"`
	OnChainAddress   string `json:"btcDepositAddress"`
	LightningAddress string `json:"lightningAddress"`
}

// CreateWallet asks the WoS API to create a brand new wallet from scratch.
// It returns a [Wallet] which can be used right away, and a set of access
// [Credentials] which should be saved in a persistent storage medium so that
// the wallet can be re-opened later with [OpenWallet].
func CreateWallet(ctx context.Context, httpClient *http.Client) (*Wallet, *Credentials, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	body := strings.NewReader("{}")
	req, err := http.NewRequestWithContext(ctx, "POST", BaseURL+"/api/v1/wallet/account", body)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("CreateWallet request failed: %w", err)
	}
	defer resp.Body.Close()

	if err := checkHTTPResponse(resp); err != nil {
		return nil, nil, fmt.Errorf("CreateWallet: %w", err)
	}

	var respStruct createWalletResponse
	if err := json.NewDecoder(resp.Body).Decode(&respStruct); err != nil {
		return nil, nil, fmt.Errorf("error decoding CreateWallet response: %w", err)
	}

	creds := &Credentials{
		APISecret: respStruct.APISecret,
		APIToken:  respStruct.APIToken,
	}

	wallet := &Wallet{
		reader:           creds.Reader(httpClient),
		signer:           creds.SimpleSigner(),
		httpClient:       httpClient,
		onChainAddress:   respStruct.OnChainAddress,
		lightningAddress: respStruct.LightningAddress,
	}

	return wallet, creds, nil
}

// LightningAddress returns the wallet's static Lightning Address.
func (wallet *Wallet) LightningAddress() string {
	return wallet.lightningAddress
}

// OnChainAddress returns the wallet's on-chain deposit address.
// Be aware this address might be reused, which is sub-optimal for privacy.
// To fetch an up-to-date address, use [Wallet.Addresses], or re-open
// the wallet.
func (wallet *Wallet) OnChainAddress() string {
	return wallet.onChainAddress
}

// SetHTTPClient updates the [http.Client] used by the wallet and its internal [Reader].
func (wallet *Wallet) SetHTTPClient(httpClient *http.Client) {
	wallet.httpClient = httpClient
	wallet.reader.httpClient = httpClient
}

// PostRequest issues an HTTP POST request to the given endpoint, authenticated by the
// Wallet's internal [Signer]. The body parameter is marshaled to JSON and sent
// as the request body.
func (wallet *Wallet) PostRequest(ctx context.Context, endpoint string, body any) ([]byte, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}
	nonce := base64.StdEncoding.EncodeToString(nonceBytes)

	hmacSignature, err := wallet.signer.SignRequest(
		ctx,
		endpoint,
		nonce,
		wallet.reader.apiToken,
		string(bodyBytes),
	)
	if err != nil {
		return nil, fmt.Errorf("Signer returned error: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", BaseURL+endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Api-Token", wallet.reader.apiToken)
	req.Header.Set("Nonce", nonce)
	req.Header.Set("Signature", hex.EncodeToString(hmacSignature))

	resp, err := wallet.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s request failed: %w", endpoint, err)
	}
	defer resp.Body.Close()

	if err := checkHTTPResponse(resp); err != nil {
		return nil, fmt.Errorf("POST %s: %w", endpoint, err)
	}

	return io.ReadAll(resp.Body)
}

// Addresses re-fetches the wallet's on-chain and lightning addresses.
// This can be useful to ensure you have the wallet's latest unused
// on-chain deposit address.
func (wallet *Wallet) Addresses(ctx context.Context) (*Addresses, error) {
	return wallet.reader.Addresses(ctx)
}

// Balance returns the current confirmed and unconfirmed balances of the wallet.
func (wallet *Wallet) Balance(ctx context.Context) (*Balance, error) {
	return wallet.reader.Balance(ctx)
}

// FeeEstimate fetches the latest fee estimation data when paying to a given on-chain
// address or lightning invoice.
func (wallet *Wallet) FeeEstimate(ctx context.Context, addressOrInvoice string) (*FeeEstimate, error) {
	return wallet.reader.FeeEstimate(ctx, addressOrInvoice)
}

// InvoiceOptions is used to customize invoices created by [Wallet.NewInvoice].
type InvoiceOptions struct {
	// Amount is the Bitcoin-denominated amount for the invoice. If not specified,
	// the payee can decide how much to pay.
	Amount float64

	// Description to include in the invoice for the payee. If omitted,
	// no description will be provided to the payee.
	Description string

	// The expiry time for the invoice, after which it can no longer be paid.
	// If omitted, defaults to 24 hours.
	Expiry time.Duration
}

type createInvoiceRequest struct {
	Amount      float64 `json:"amount"`
	Description string  `json:"description,omitempty"`
	Expiry      uint    `json:"expiry,omitempty"`
}

// Invoice is a Bitcoin Lightning invoice returned by the WoS API.
type Invoice struct {
	// ID is a UUID which identifies the invoice.
	ID string `json:"id"`

	// Bolt11 is the [BOLT11] serialized invoice. This is what should
	// be displayed to the user or encoded in QR codes.
	Bolt11 string `json:"invoice"`

	// Amount is the Bitcoin amount encoded in the invoice.
	Amount float64 `json:"btcAmount"`

	// Expires is the expiry time at which the invoice is no longer payable.
	Expires time.Time `json:"expires"`
}

// NewInvoice creates a new [BOLT11] payment invoice, essentially a request for payment.
//
// The [InvoiceOptions] argument customizes the invoice. opts can be nil, which creates a
// variable-amount invoice with no description and a 24-hour expiry.
//
// [BOLT11]: https://github.com/lightning/bolts/blob/master/11-payment-encoding.md
func (wallet *Wallet) NewInvoice(ctx context.Context, opts *InvoiceOptions) (*Invoice, error) {
	if opts == nil {
		opts = &InvoiceOptions{}
	}

	if opts.Amount < 0 {
		return nil, fmt.Errorf("invalid invoice amount: %f", opts.Amount)
	} else if opts.Expiry < 0 {
		return nil, fmt.Errorf("invalid invoice expiry time: %s", opts.Expiry)
	}

	request := createInvoiceRequest{
		Amount:      opts.Amount,
		Description: opts.Description,
		Expiry:      uint(opts.Expiry.Seconds()),
	}

	respData, err := wallet.PostRequest(ctx, "/api/v1/wallet/createInvoice", request)
	if err != nil {
		return nil, fmt.Errorf("NewInvoice: %w", err)
	}

	var invoice Invoice
	if err := json.Unmarshal(respData, &invoice); err != nil {
		return nil, fmt.Errorf("invalid NewInvoice response: %w", err)
	}

	return &invoice, nil
}

type sendPaymentRequest struct {
	Address      string  `json:"address"`
	Currency     string  `json:"currency"`
	Amount       float64 `json:"amount,omitempty"`
	Description  string  `json:"description,omitempty"`
	MaxLightning bool    `json:"sendMaxLightning,omitempty"`
	MaxBitcoin   bool    `json:"sendMaxBtc,omitempty"`
}

func (wallet *Wallet) newPayment(
	ctx context.Context,
	method string,
	req sendPaymentRequest,
) (*Payment, error) {
	respData, err := wallet.PostRequest(ctx, "/api/v1/wallet/payment", req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}

	var payment Payment
	if err := json.Unmarshal(respData, &payment); err != nil {
		return nil, fmt.Errorf("invalid %s response: %w", method, err)
	}
	return &payment, nil
}

// PayInvoice executes a payment to a given lightning invoice. The description is
// stored in the WoS payment history.
//
// Returns an error wrapping [ErrInvalidInvoice] if the invoice is not valid.
//
// If the invoice does not specify a fixed amount, this method returns an error
// wrapping [ErrNoAmount]. To pay a variable-amount invoice, use [Wallet.PayVariableInvoice].
//
// To estimate fees, use [Wallet.FeeEstimate] or [Reader.FeeEstimate].
func (wallet *Wallet) PayInvoice(ctx context.Context, invoice, description string) (*Payment, error) {
	amount, err := parseInvoiceAmount(invoice)
	if err != nil {
		return nil, fmt.Errorf("PayInvoice: %w", err)
	}

	return wallet.newPayment(ctx, "PayInvoice", sendPaymentRequest{
		Address:     invoice,
		Currency:    "LIGHTNING",
		Description: description,
		Amount:      amount,
	})
}

// PayVariableInvoice executes a payment to a given variable-amount lightning invoice.
// The description is stored in the WoS payment history.
//
// Returns an error wrapping [ErrInvalidInvoice] if the invoice is not valid.
//
// Returns an error wrapping [ErrFixedAmount] if the invoice specifies a fixed amount.
// In this case, you should use [Wallet.PayInvoice].
//
// To estimate fees, use [Wallet.FeeEstimate] or [Reader.FeeEstimate].
func (wallet *Wallet) PayVariableInvoice(
	ctx context.Context,
	invoice string,
	description string,
	amount float64,
) (*Payment, error) {
	_, err := parseInvoiceAmount(invoice)
	if err != nil && !errors.Is(err, ErrNoAmount) {
		return nil, fmt.Errorf("PayVariableInvoice: %w", err)
	} else if err == nil {
		return nil, fmt.Errorf("PayVariableInvoice: %w", err)
	}

	return wallet.newPayment(ctx, "PayInvoice", sendPaymentRequest{
		Address:     invoice,
		Currency:    "LIGHTNING",
		Description: description,
		Amount:      amount,
	})
}

// PayOnChain executes an on-chain payment transaction, paying amount to the given address.
// The description is stored in the WoS payment history.
//
// To estimate fees, use [Wallet.FeeEstimate] or [Reader.FeeEstimate].
func (wallet *Wallet) PayOnChain(
	ctx context.Context,
	address string,
	amount float64,
	description string,
) (*Payment, error) {
	return wallet.newPayment(ctx, "PayOnChain", sendPaymentRequest{
		Address:     address,
		Currency:    "BTC",
		Description: description,
		Amount:      amount,
	})
}

// SweepLightning executes a lightning payment, sweeping the entire available lightning balance
// to a given variable-amount invoice. The description is stored in the WoS payment history.
//
// Returns an error wrapping [ErrInvalidInvoice] if the invoice is not valid.
//
// Returns an error wrapping [ErrFixedAmount] if the invoice embeds a fixed amount.
func (wallet *Wallet) SweepLightning(ctx context.Context, invoice, description string) (*Payment, error) {
	if _, err := parseInvoiceAmount(invoice); !errors.Is(err, ErrNoAmount) {
		return nil, fmt.Errorf("SweepLightning: %w", ErrFixedAmount)
	}

	balance, fees, err := wallet.reader.BalanceAndFee(ctx, invoice)
	if err != nil {
		return nil, fmt.Errorf("SweepLightning: %w", err)
	}

	return wallet.newPayment(ctx, "SweepLightning", sendPaymentRequest{
		Address:      invoice,
		Currency:     "LIGHTNING",
		Description:  description,
		MaxLightning: true,
		Amount:       balance.Confirmed - fees.MaxLightningFee,
	})
}

// SweepOnChain executes an on-chain payment transaction, sweeping the entire available wallet
// balance to a given on-chain address. The description is stored in the WoS payment history.
func (wallet *Wallet) SweepOnChain(ctx context.Context, address, description string) (*Payment, error) {
	balance, fees, err := wallet.reader.BalanceAndFee(ctx, address)
	if err != nil {
		return nil, fmt.Errorf("SweepOnChain: %w", err)
	}

	availableBalance := balance.Confirmed - fees.BtcFixedFee
	if availableBalance < 0 {
		return nil, fmt.Errorf(
			"confirmed balance (%.8f) insufficient for fixed fee (%.8f)",
			balance.Confirmed,
			fees.BtcFixedFee,
		)
	}

	commission := fees.BtcSendCommissionPercent * balance.Confirmed
	amount := availableBalance - commission
	if amount <= 0 {
		return nil, fmt.Errorf(
			"available balance (%.8f) insufficient for commission (%.8f)",
			availableBalance,
			commission,
		)
	}

	return wallet.newPayment(ctx, "SweepOnChain", sendPaymentRequest{
		Address:     address,
		Currency:    "BTC",
		Description: description,
		MaxBitcoin:  true,
		Amount:      amount,
	})
}
