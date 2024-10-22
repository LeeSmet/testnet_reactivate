# Testnet Reactivate

Go program to activate Threefold testnet accounts again. The program expects a file called `testnet_secrets.csv`,
with the address and secret of every service. The file is provided in the repo with secrets redacted.

This script also attempts to fund the devnet and qa net TFT bridge with 1M TFT. For this to work, the respective bridges
have to have a TFT trustline setup already.
