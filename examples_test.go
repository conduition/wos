package wos_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/conduition/wos"
)

func ExampleCreateWallet() {
	wallet, creds, err := wos.CreateWallet(context.Background(), nil)
	if err != nil {
		panic(err)
	}

	fmt.Println(wallet.LightningAddress())
	fmt.Printf("API token:  %s\n", creds.APIToken)
	fmt.Printf("API secret: %s\n", creds.APISecret)
}

func ExampleReader() {
	reader := wos.NewReader("edcc867c-96ff-4b0d-ba68-165c16071de0", nil)
	addresses, err := reader.Addresses(context.Background())
	if err != nil {
		panic(err)
	}

	fmt.Println(addresses.Lightning)

	// output:
	// dorsalpuma54@walletofsatoshi.com
}

func ExampleCredentials_OpenWallet() {
	creds := wos.Credentials{
		APIToken:  "edcc867c-96ff-4b0d-ba68-165c16071de0",
		APISecret: "91ul0rDKV1gANhQWWyEXhdWaSa6aQwAF",
	}

	wallet, err := creds.OpenWallet(context.Background(), nil)
	if err != nil {
		panic(err)
	}

	fmt.Println(wallet.LightningAddress().String())

	// output:
	// dorsalpuma54@walletofsatoshi.com
}

func ExampleWallet_PayLightningAddress() {
	creds := wos.Credentials{
		APIToken:  "edcc867c-96ff-4b0d-ba68-165c16071de0",
		APISecret: "91ul0rDKV1gANhQWWyEXhdWaSa6aQwAF",
	}

	ctx := context.Background()
	wallet, err := creds.OpenWallet(ctx, nil)
	if err != nil {
		panic(err)
	}

	// smallwillow98@walletofsatoshi.com
	// API token:  6edf02b8-d4e9-4640-b7e4-90bc97f476ab
	// API secret: sgN5hn2RibvSba1vv260NvwnwVy0oiuh

	lnAddress, err := wos.ParseLightningAddress("smallwillow98@walletofsatoshi.com")
	if err != nil {
		panic(err)
	}

	payment, err := wallet.PayLightningAddress(ctx, lnAddress, "", 0.00000001)
	if err != nil {
		panic(err)
	}

	fmt.Println(payment.Status)
}

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
		// apiToken omitted for security; assume the remote knows it already.
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

func ExampleSigner() {
	reader := wos.NewReader("93b9c574-30a2-4bf5-81ba-f9feadb313a7", nil)
	signer := RemoteSigner{"https://somewheresecure.place/api/sign"}
	wallet, err := wos.OpenWallet(context.Background(), reader, signer)
	if err != nil {
		panic(err)
	}
	fmt.Println(wallet.LightningAddress())
}
