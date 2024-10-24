package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/pkg/errors"
	"github.com/stellar/go/clients/horizonclient"
	"github.com/stellar/go/keypair"
	"github.com/stellar/go/network"
	"github.com/stellar/go/txnbuild"
)

const (
	BaseFee = 1_000_000 // 0.1XLM

	TftIssuer     = "GA47YZA3PKFUZMPLQ3B5F2E3CJIB57TGGU7SPCQT2WAEYKN766PWIMB3"
	TftaIssuer    = "GB55A4RR4G2MIORJTQA4L6FENZU7K4W7ATGY6YOT2CW47M5SZYGYKSCT"
	FreeTftIssuer = "GBLDUINEFYTF7XEE7YNWA3JQS4K2VD37YU7I2YAE7R5AHZDKQXSS2J6R"

	InputFile = "testnet_secrets.csv"

	HomePageDomain = "www2.threefold.io"

	DevnetBridge = "GDHJP6TF3UXYXTNEZ2P36J5FH7W4BJJQ4AYYAXC66I2Q2AH5B6O6BCFG"
	QaNetBridge  = "GAQH7XXFBRWXT2SBK6AHPOLXDCLXVFAKFSOJIRMRNCDINWKHGI6UYVKM"
)

var (
	Issuers = []string{"TFT issuer", "TFTA issuer", "FreeTFT issuer"}
	// Bridge addresses, Devnet and QA net
	DevnetBridgeSigners = []string{"GDRVBYUUP5NGH5VDMKXP3SOIU4TRNHE2XI372UC24ZL2KLKHE2KQTY2E", "GCUCIV7SG4R2Z5M3A3U5EU3PLEJKQJI5M2HDYZUDLDXDVFBJ3REJL6VP"}
)

func main() {
	input, err := os.ReadFile(InputFile)
	if err != nil {
		panic(err)
	}

	keys := map[string]keypair.KP{}
	for _, line := range strings.Split(string(input), "\n") {
		fields := strings.Split(line, ",")
		if len(fields) != 3 {
			break
		}
		kp := keypair.MustParseFull(fields[2])
		// Sanity check
		if kp.Address() != fields[1] {
			panic("Address in file does not match derived address")
		}
		keys[fields[0]] = kp
	}

	// First activate all accounts. If we get an error just log and continue
	// as this will only work the first time
	// Also set up the tokens
	for _, kp := range keys {
		if err = activateThroughFriendbot(kp); err != nil {
			fmt.Println("failed to activate account", err)
		}
	}

	for _, name := range Issuers {
		kp, found := keys[name]
		if !found {
			panic("Token issuer account not found")
		}

		fmt.Println("Add token homepage for", kp.Address())
		err := addTokenHomePage(kp, HomePageDomain)
		if err != nil {
			fmt.Println("failed to add token homepage", err)
		}
	}

	fmt.Println("Add trustlines")

	for name, key := range keys {
		isIssuer := false
		for _, issuer := range Issuers {
			if issuer == name {
				isIssuer = true
			}
		}
		if isIssuer {
			continue
		}

		fmt.Println("Add trustline for", key.Address())
		if err := addTrustlines(key); err != nil {
			fmt.Println("failed to add trustline for key", key.Address(), err)
		}
	}

	// This requires the bridges to have a trustline for TFT
	fmt.Println("Funding brides")

	if err := fundBridges(keys["TFT issuer"]); err != nil {
		fmt.Println("Failed to fund bridges", err)
	}

	fmt.Println("Setup devnet bridge signers")
	if err := setupDevnetBridgeSigners(keys["DevnetBridge"], DevnetBridgeSigners); err != nil {
		fmt.Println("Failed to fund bridges", err)
	}
}

func setupDevnetBridgeSigners(pair keypair.KP, signers []string) error {
	address := pair.Address()

	request := horizonclient.AccountRequest{AccountID: address}
	account, err := horizonclient.DefaultTestNetClient.AccountDetail(request)
	if err != nil {
		return errors.Wrap(err, "could not load account")
	}

	ops := []txnbuild.Operation{}
	for _, address := range signers {
		op := txnbuild.SetOptions{
			Signer: &txnbuild.Signer{
				Address: address,
				Weight:  1,
			},
		}
		ops = append(ops, &op)
	}

	// Now that we have multiple signers adjust the weight
	ops = append(ops, &txnbuild.SetOptions{
		LowThreshold:    txnbuild.NewThreshold(1),
		MediumThreshold: txnbuild.NewThreshold(2),
		HighThreshold:   txnbuild.NewThreshold(2),
	})

	txparams := txnbuild.TransactionParams{
		SourceAccount:        &txnbuild.SimpleAccount{AccountID: address, Sequence: account.Sequence},
		IncrementSequenceNum: true,
		Operations:           ops,
		BaseFee:              BaseFee,
		Memo:                 nil,
		Preconditions: txnbuild.Preconditions{
			TimeBounds: txnbuild.NewTimeout(60),
		},
	}

	tx, err := txnbuild.NewTransaction(txparams)
	if err != nil {
		return errors.Wrap(err, "could not generate transaction")
	}

	tx, err = tx.Sign(network.TestNetworkPassphrase, pair.(*keypair.Full))
	if err != nil {
		return errors.Wrap(err, "could not sign transaction")
	}

	_, err = horizonclient.DefaultTestNetClient.SubmitTransaction(tx)
	if err != nil {
		return errors.Wrap(err, "could not submit set domain tx")
	}

	return nil
}

func fundBridges(pair keypair.KP) error {
	address := pair.Address()

	request := horizonclient.AccountRequest{AccountID: address}
	account, err := horizonclient.DefaultTestNetClient.AccountDetail(request)
	if err != nil {
		return errors.Wrap(err, "could not load account")
	}

	ops := []txnbuild.Operation{}
	for _, addr := range []string{DevnetBridge, QaNetBridge} {
		op := txnbuild.Payment{
			Destination: addr,
			// 1M TFT
			Amount: "1000000",
			Asset: txnbuild.CreditAsset{
				Code:   "TFT",
				Issuer: TftIssuer,
			},
		}

		ops = append(ops, &op)
	}

	txparams := txnbuild.TransactionParams{
		SourceAccount:        &txnbuild.SimpleAccount{AccountID: address, Sequence: account.Sequence},
		IncrementSequenceNum: true,
		Operations:           ops,
		BaseFee:              BaseFee,
		Memo:                 nil,
		Preconditions: txnbuild.Preconditions{
			TimeBounds: txnbuild.NewTimeout(60),
		},
	}

	tx, err := txnbuild.NewTransaction(txparams)
	if err != nil {
		return errors.Wrap(err, "could not generate transaction")
	}

	tx, err = tx.Sign(network.TestNetworkPassphrase, pair.(*keypair.Full))
	if err != nil {
		return errors.Wrap(err, "could not sign transaction")
	}

	_, err = horizonclient.DefaultTestNetClient.SubmitTransaction(tx)
	if err != nil {
		return errors.Wrap(err, "could not submit add trust tx")
	}

	return nil
}

func addTrustlines(pair keypair.KP) error {
	address := pair.Address()

	request := horizonclient.AccountRequest{AccountID: address}
	account, err := horizonclient.DefaultTestNetClient.AccountDetail(request)
	if err != nil {
		return errors.Wrap(err, "could not load account")
	}

	opTft := txnbuild.ChangeTrust{
		Line: txnbuild.ChangeTrustAssetWrapper{Asset: txnbuild.CreditAsset{
			Code:   "TFT",
			Issuer: TftIssuer,
		}},
	}
	opTfta := txnbuild.ChangeTrust{
		Line: txnbuild.ChangeTrustAssetWrapper{Asset: txnbuild.CreditAsset{
			Code:   "TFTA",
			Issuer: TftaIssuer,
		}},
	}
	opFreeTFT := txnbuild.ChangeTrust{
		Line: txnbuild.ChangeTrustAssetWrapper{Asset: txnbuild.CreditAsset{
			Code:   "FreeTFT",
			Issuer: FreeTftIssuer,
		}},
	}

	if err = opTft.Validate(); err != nil {
		return errors.Wrap(err, "could not validate add trust op")
	}
	if err = opTfta.Validate(); err != nil {
		return errors.Wrap(err, "could not validate add trust op")
	}
	if err = opFreeTFT.Validate(); err != nil {
		return errors.Wrap(err, "could not validate add trust op")
	}

	txparams := txnbuild.TransactionParams{
		SourceAccount:        &txnbuild.SimpleAccount{AccountID: address, Sequence: account.Sequence},
		IncrementSequenceNum: true,
		Operations:           []txnbuild.Operation{&opTft, &opTfta, &opFreeTFT},
		BaseFee:              BaseFee,
		Memo:                 nil,
		Preconditions: txnbuild.Preconditions{
			TimeBounds: txnbuild.NewTimeout(60),
		},
	}

	tx, err := txnbuild.NewTransaction(txparams)
	if err != nil {
		return errors.Wrap(err, "could not generate transaction")
	}

	tx, err = tx.Sign(network.TestNetworkPassphrase, pair.(*keypair.Full))
	if err != nil {
		return errors.Wrap(err, "could not sign transaction")
	}

	_, err = horizonclient.DefaultTestNetClient.SubmitTransaction(tx)
	if err != nil {
		return errors.Wrap(err, "could not submit add trust tx")
	}

	return nil
}

func addTokenHomePage(issuer keypair.KP, domain string) error {
	address := issuer.Address()

	request := horizonclient.AccountRequest{AccountID: address}
	account, err := horizonclient.DefaultTestNetClient.AccountDetail(request)
	if err != nil {
		return errors.Wrap(err, "could not load account")
	}

	op := txnbuild.SetOptions{
		HomeDomain: &domain,
	}

	txparams := txnbuild.TransactionParams{
		SourceAccount:        &txnbuild.SimpleAccount{AccountID: address, Sequence: account.Sequence},
		IncrementSequenceNum: true,
		Operations:           []txnbuild.Operation{&op},
		BaseFee:              BaseFee,
		Memo:                 nil,
		Preconditions: txnbuild.Preconditions{
			TimeBounds: txnbuild.NewTimeout(60),
		},
	}

	tx, err := txnbuild.NewTransaction(txparams)
	if err != nil {
		return errors.Wrap(err, "could not generate transaction")
	}

	tx, err = tx.Sign(network.TestNetworkPassphrase, issuer.(*keypair.Full))
	if err != nil {
		return errors.Wrap(err, "could not sign transaction")
	}

	_, err = horizonclient.DefaultTestNetClient.SubmitTransaction(tx)
	if err != nil {
		return errors.Wrap(err, "could not submit set domain tx")
	}

	return nil
}

func activateThroughFriendbot(pair keypair.KP) error {
	// pair is the pair that was generated from previous example, or create a pair based on
	// existing keys.
	address := pair.Address()
	resp, err := http.Get("https://friendbot.stellar.org/?addr=" + address)
	if err != nil {
		return errors.Wrap(err, "could not call friendbot")
	}

	if resp.StatusCode != 200 {
		return errors.Errorf("got unexpected status code %d from friendbot", resp.StatusCode)
	}

	defer resp.Body.Close()
	_, err = io.ReadAll(resp.Body)
	if err != nil {
		return errors.Wrap(err, "could not read friendbot response")
	}

	return nil
}
