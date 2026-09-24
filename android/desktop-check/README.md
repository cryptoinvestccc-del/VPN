# Running the app's code without a phone

`Uapi.java` decides what the tunnel engine is told. It used to be checked
by reading it, which is how `i1` came to be treated as a number: the
packet templates AmneziaWG calls I1 to I5 are hex, and a number parse
dropped them without a word.

`build.sh` compiles that one class for an ordinary JVM. The test
`TestAppAndHarnessAgree` in `test/livetunnel` then runs it and compares
its output, line for line, with the configuration the harness builds —
the one `TestShippedEngineCarriesTraffic` proves carries traffic through
the ARM binary that ships inside the APK.

    ./android/desktop-check/build.sh
    cd test/livetunnel && go test ./...

Nothing here ships. It exists so a change to the configuration format
fails on this machine rather than on somebody's phone.
