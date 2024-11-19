module github.com/jeffwilliams/anvil

go 1.23.2

replace github.com/sarpdag/boyermoore => github.com/jeffwilliams/boyermoore v0.0.0-20220817021623-63ad6ff520f8

// This is to get a fix that is not merged into the upstream crypto yet
replace golang.org/x/crypto => github.com/jeffwilliams/go-x-crypto v0.0.0-20240728132943-bda27f7d305d

// replace gioui.org => /path/to/local/gio

// This is so we can get the commit that fixes unifont, and my change to allow the
// Command key to be send as a keypress on MacOS
replace gioui.org => github.com/jeffwilliams/gio v0.7.2

require (
	gioui.org v0.7.1
	github.com/UserExistsError/conpty v0.1.4
	github.com/acarl005/stripansi v0.0.0-20180116102854-5a71ef0e047d
	github.com/alecthomas/chroma v0.10.0
	github.com/alecthomas/chroma/v2 v2.0.0-alpha4
	github.com/armon/go-radix v1.0.0
	github.com/crazy3lf/colorconv v1.2.0
	github.com/creack/pty v1.1.21
	github.com/flopp/go-findfont v0.1.0
	github.com/gorilla/websocket v1.5.3
	github.com/jeffwilliams/syn v0.1.7
	github.com/jszwec/csvutil v1.6.0
	github.com/leaanthony/go-ansi-parser v1.6.1
	github.com/ogier/pflag v0.0.1
	github.com/pelletier/go-toml v1.9.5
	github.com/pkg/profile v1.6.0
	github.com/sarpdag/boyermoore v0.0.0-20210425165139-a89ed1b5913b
	github.com/speedata/hyphenation v1.0.2
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.8.4
	golang.org/x/crypto v0.23.0
	golang.org/x/image v0.18.0
	golang.org/x/sys v0.22.0
)

require (
	gioui.org/cpu v0.0.0-20210817075930-8d6a761490d2 // indirect
	gioui.org/shader v1.0.8 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dlclark/regexp2 v1.11.0 // indirect
	github.com/go-text/typesetting v0.1.2 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rivo/uniseg v0.2.0 // indirect
	golang.org/x/exp v0.0.0-20240707233637-46b078467d37 // indirect
	golang.org/x/exp/shiny v0.0.0-20240707233637-46b078467d37 // indirect
	golang.org/x/text v0.20.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
