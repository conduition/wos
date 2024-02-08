package wos

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/conduition/wos/bech32"
)

var (
	// ErrInvalidInvoice is returned when an invalid invoice is received.
	ErrInvalidInvoice = errors.New("invalid invoice")

	// ErrNoAmount is returned when paying an invoice which has no amount specified.
	ErrNoAmount = errors.New("no amount specified in invoice")

	// ErrFixedAmount is returned when attempting to pay a fixed-amount invoice
	// as if it were a variable amount invoice.
	ErrFixedAmount = errors.New("invoice specifies a fixed amount")
)

// decodeAmount returns the amount encoded by the provided string in
// millisatoshi.
func decodeAmount(amount string) (uint64, error) {
	if len(amount) < 1 {
		return 0, fmt.Errorf("amount must be non-empty")
	}

	// If last character is a digit, then the amount can just be
	// interpreted as BTC.
	lastHRPChar := amount[len(amount)-1]
	digit := lastHRPChar - '0'
	if digit >= 0 && digit <= 9 {
		btc, err := strconv.ParseUint(amount, 10, 64)
		if err != nil {
			return 0, err
		}
		return btc * 100_000_000 * 1000, nil
	}

	num := amount[:len(amount)-1]
	if len(num) < 1 {
		return 0, fmt.Errorf("number must be non-empty")
	}

	am, err := strconv.ParseUint(num, 10, 64)
	if err != nil {
		return 0, err
	}

	// If not a digit, it must be part of the known units.
	switch lastHRPChar {
	case 'p':
		if am < 10 {
			return 0, fmt.Errorf("minimum amount is 10p")
		}
		if am%10 != 0 {
			return 0, fmt.Errorf("amount %d pBTC not expressible in msat", am)
		}
		return am / 10, nil

	case 'n':
		return am * 100, nil
	case 'u':
		return am * 100_000, nil
	case 'm':
		return am * 100_000_000, nil

	default:
		return 0, fmt.Errorf("unknown multiplier %c", lastHRPChar)
	}
}

func parseInvoiceAmount(invoice string) (float64, error) {
	hrp, _, err := bech32.DecodeNoLimit(invoice)
	if err != nil {
		return 0, fmt.Errorf("%w: %s", ErrInvalidInvoice, err)
	}

	if len(hrp) < 3 {
		return 0, ErrInvalidInvoice
	}

	if hrp[:2] != "ln" {
		return 0, ErrInvalidInvoice
	}

	firstNumber := strings.IndexAny(hrp, "1234567890")
	if firstNumber == -1 {
		return 0, ErrNoAmount
	}

	chainPrefix := strings.ToLower(hrp[2:firstNumber])
	if chainPrefix != "bc" {
		return 0, fmt.Errorf("%w: invoice is not for bitcoin mainnet", ErrInvalidInvoice)
	}

	msat, err := decodeAmount(hrp[firstNumber:])
	if err != nil {
		return 0, fmt.Errorf("%w: invalid amount: %s", ErrInvalidInvoice, err)
	}

	btc := math.Round(float64(msat)/1000) / 100_000_000
	return btc, nil
}
