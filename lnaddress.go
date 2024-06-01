package wos

import (
	"errors"
	"regexp"
	"strings"
)

// ErrInvalidLightningAddress is returned when parsing an invalid lightning address.
var ErrInvalidLightningAddress = errors.New("invalid lightning address")

// This misses some edgecases. Might need to adjust in future.
// https://stackoverflow.com/a/67686133
var emailRegex = regexp.MustCompile(`^[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,4}$`)

// LightningAddress is a `user@domain.tld` internet identifier which
// allows senders to request lightning invoices by contacting `domain.tld`,
// who issues invoices on  behalf of the `user`.
type LightningAddress struct {
	Username string
	Domain   string
}

// String returns the user@domain.tld format of the address.
func (a LightningAddress) String() string {
	return a.Username + "@" + a.Domain
}

// LNURL returns the HTTPS URL used for LNURL payRequest, as per LUD-16.
//
// https://github.com/lnurl/luds/blob/luds/16.md
func (a LightningAddress) LNURL() string {
	return "https://" + a.Domain + "/.well-known/lnurlp/" + a.Username
}

// ParseLightningAddress parses a [LightningAddress] from a string, returning
// ErrInvalidLightningAddress if the address is not a valid identifier.
func ParseLightningAddress(lnAddress string) (LightningAddress, error) {
	if !emailRegex.MatchString(lnAddress) {
		return LightningAddress{}, ErrInvalidLightningAddress
	}

	i := strings.Index(lnAddress, "@")
	username, domain := lnAddress[:i], lnAddress[i+1:]

	addr := LightningAddress{
		Username: username,
		Domain:   domain,
	}

	return addr, nil
}
