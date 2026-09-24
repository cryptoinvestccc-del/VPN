# Checking the whole thing without a phone

Three tests, in the order they were needed.

**`TestShippedEngineCarriesTraffic`** runs the exact ARM binary that
travels inside the APK — emulated, not rebuilt for this machine — over a
real kernel tun device, with the descriptor handed across `exec` the way
the app hands it across. It asks that binary for its own keypair, gives
it the configuration the app itself built, and then sends a message
through the kernel, into the engine, out the far side and back. Breaking
one obfuscation header makes it fail, which is how we know it is
measuring something.

**`TestWholePathWithoutAPhone`** runs the provisioning service and the
credential it hands out, against an AmneziaWG server carrying the same
obfuscation the real one does.

**`TestAppAndHarnessAgree`** compiles the app's own configuration class
for a desktop JVM and compares its output, line for line, with the
harness's. Run `android/desktop-check/build.sh` first, or it skips.

What none of this covers is Android's own half: `VpnService.establish()`
producing the descriptor, and Android permitting execution from the
library directory. Everything else is here.

    sudo go test ./...          # the tun device needs it

`livetunnel` itself is the same path against a running deployment:

    go run . -endpoint https://besyvpn.online/v1/issue
