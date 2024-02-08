# `wos`

API client for the Wallet of Satoshi Bitcoin Lightning app.

[Wallet of Satoshi](https://walletofsatoshi.com/) is a custodial Bitcoin Lightning wallet app. It is effectively a [web-wallet](https://www.whatisbitcoin.com/learn/what-is-a-web-wallet), because the signing keys are actually hosted on WoS servers, while their mobile app is just a thin API client around their backend sevice.

By using WoS, Bitcoiners trade security for ease-of-use. WoS is well known for being a very beginner-friendly Lightning wallet, due largely to this trade-off. WoS can run off with your money, but you also don't have to worry about running a node, managing channels, updating software, and so forth.

Since WoS is a no-KYC no-signup-required web-wallet, it is very easy to reverse-engineer their API for programmatic use. New wallets can be created on-the-fly with no API credentials needed. Existing wallets can be accessed using simple API credentials.

This library is a Golang package which encapsulates the WoS v1 REST API.

## Usage

The `Wallet` struct type provides a full interface to the WoS API, including creating invoices and sending payments both on-chain and over Lightning.

```golang
package main

import (
  "context"
  "fmt"
  "os"

  "github.com/conduition/wos"
)

func main() {
  ctx := context.Background()

  // First, create a wallet from scratch. It will have empty balances
  // but you can start depositing right away via lightning.
  wallet, creds, err := wos.CreateWallet(ctx, nil)
  if err != nil {
    panic(err)
  }
  fmt.Println(wallet.LightningAddress())

  // The Credentials should be saved somewhere, so that you can
  // regain access to the same wallet later.
  os.WriteFile(
    "/secure/location/wos-creds",
    []byte(creds.APIToken+"\n"+creds.APISecret),
    0o600,
  )

  // To reopen the wallet after going offline, parse the Credentials
  // from the disk, and then use Credentials.OpenWallet.
  wallet, err = creds.OpenWallet(ctx, nil)
  if err != nil {
    panic(err)
  }

  // Create an invoice.
  invoice, err := wallet.NewInvoice(ctx, &wos.InvoiceOptions{
    Amount:      0.0001,
    Description: "don't actually send money to this invoice.",
  })
  if err != nil {
    panic(err)
  }
  fmt.Println(invoice.Bolt11)

  // Pay an invoice.
  payment, err := wallet.PayInvoice(ctx, invoice.Bolt11, "a payment label, can be omitted")
  if err != nil {
    panic(err)
  }
  fmt.Println(payment.Status, payment.Amount, payment.Currency, payment.Time)
}
```

## Segregated Credentials

WoS credentials are split into a bearer _API token_ and a shared _API secret._

The token is passed as a header with every HTTP request to the WoS API, while the secret is used to produce HMACs for POST requests.

The secret-signature is only required for POST requests which change wallet state - such as creating or paying invoices, GET requests - such as fetching balance or payment history - require only the _API token._ This means a WoS API client can be segregated into a `Reader` and a `Signer`.

A `Reader` can view a WoS account's balances and ongoing payments in real-time, while A `Signer` is an interface type which can be a simple wrapper around the API Secret, or the API secret could live offline or on a more secure machine which validates & signs POST requests, enforcing arbitrary user-defined rules (e.g. only allow max $50 per purchase, or max $1000 per day, etc). Put both together and you get a `Wallet`.

The `wos` package fully supports this kind of architecture. For example, consider this example with a `Signer` which lives on a remote machine. Signatures are fetched via HTTP POST requests.

```golang
package main

import (
  "bytes"
  "context"
  "encoding/json"
  "fmt"
  "io"
  "net/http"

  "github.com/conduition/wos"
)

type RemoteSigner struct {
  URL string
}

func (rs RemoteSigner) SignRequest(
  ctx context.Context,
  endpoint, nonce, requestBody, apiToken string,
) ([]byte, error) {
  bodyBytes, err := json.Marshal(map[string]string{
    "endpoint": endpoint,
    "nonce":    nonce,
    "body":     requestBody,
  })
  if err != nil {
    return nil, err
  }

  req, err := http.NewRequestWithContext(ctx, "POST", rs.URL, bytes.NewReader(bodyBytes))
  if err != nil {
    return nil, err
  }
  req.Header.Set("Content-Type", "application/json")

  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    return nil, err
  }
  defer resp.Body.Close()

  if resp.StatusCode != 200 {
    return nil, fmt.Errorf("received status code %d from remote signer", resp.StatusCode)
  }

  return io.ReadAll(resp.Body)
}

func main() {
  reader := wos.NewReader("93b9c574-30a2-4bf5-81ba-f9feadb313a7", nil)
  signer := RemoteSigner{"https://somewheresecure.place/api/sign"}
  wallet, err := wos.OpenWallet(context.Background(), reader, signer)
  if err != nil {
    panic(err)
  }
  fmt.Println(wallet.LightningAddress())
}
```
