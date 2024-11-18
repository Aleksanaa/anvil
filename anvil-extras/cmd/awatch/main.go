package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	api "anvil-go-api"

	"github.com/ogier/pflag"
)

var (
	noBody   io.Reader
	httpApi  api.Anvil
	cmds     = []string{}
	watchWin api.Window
)

var (
	optDebug = pflag.BoolP("debug", "d", false, "Print debug messages")
)

func main() {
	pflag.Parse()

	var err error
	httpApi, err = api.NewFromEnv()
	dieIfError(err, "connecting to API failed")

	handlers := api.WebsockHandlers{
		Notification: handlePutNotification,
	}

	wsApi, err := httpApi.Websock(handlers)
	dieIfError(err, "creating websocket failed")

	loadFirstCommand()
	watchWin = findOrCreateWindow(&httpApi, watchPath())

	runCmdsAndUpdateWindow()

	wsApi.Run()
}

func debug(format string, args ...interface{}) {
	if !*optDebug {
		return
	}
	fmt.Printf(format, args...)
}

func dieIfError(err error, msg string) {
	if err != nil {
		msg := fmt.Sprintf("%s: %s", msg, err)
		die(msg)
	}
}

func die(msg string) {
	fmt.Fprintf(os.Stderr, "awatch: %s\n", msg)
	os.Exit(1)
}

func loadFirstCommand() {
	if len(pflag.Args()) < 1 {
		die("no arguments were passed. The arguments must be a command to run")
	}

	cmds = append(cmds, strings.Join(pflag.Args(), " "))
}

func run(cmd string) (output []byte, err error) {
	c := newCmd(cmd)
	return c.CombinedOutput()
}

func findOrCreateWindow(anvil *api.Anvil, watchPath string) api.Window {
	var wins []api.Window
	err := anvil.GetInto("/wins", &wins)
	dieIfError(err, "reading windows failed")
	for _, w := range wins {
		if w.Path == watchPath {
			return w
		}
	}

	win := createNewWindow(anvil)
	setWindowTag(anvil, win.Id, watchPath)
	return win
}

func createNewWindow(anvil *api.Anvil) api.Window {
	rsp, err := anvil.Post("/wins", noBody)
	dieIfError(err, "creating new window failed")

	raw, err := ioutil.ReadAll(rsp.Body)
	dieIfError(err, "reading response from creating window failed")

	var win api.Window
	err = json.Unmarshal(raw, &win)
	dieIfError(err, "decoding JSON response after creating window failed")
	return win
}

func watchPath() string {
	anvilGlobalPath := os.Getenv("ANVIL_WIN_GLOBAL_DIR")
	return filepath.Join(anvilGlobalPath, "+watch")
}

func setWindowTag(anvil *api.Anvil, winId int, watchPath string) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%s Del! Snarf | Look ", watchPath)
	anvil.Put(fmt.Sprintf("/wins/%d/tag", winId), &buf)
}

func handlePutNotification(notif *api.Notification, err error) {
	if err != nil {
		// Parsing notification failed.
		fmt.Fprintf(os.Stderr, "awatch: parsing notification failed: %v\n", err)
		return
	}

	if notif.Op != api.NotificationOpPut {
		return
	}

	debug("awatch: got put notification for window %d\n", notif.WinId)

	var info api.Window
	err = httpApi.GetInto(fmt.Sprintf("/wins/%d/info", notif.WinId), &info)
	if err != nil {
		// Parsing notification failed.
		fmt.Fprintf(os.Stderr, "awatch: getting info for window %d failed: %v\n", notif.WinId, err)
		return
	}

	localDir, err := filepath.Abs(os.Getenv("ANVIL_WIN_LOCAL_DIR"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "awatch: getting absolute path of %s failed: %v\n", os.Getenv("ANVIL_WIN_LOCAL_DIR"))
		return
	}

	winPath, err := filepath.Abs(info.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "awatch: getting absolute path of %s failed: %v\n", info.Path)
		return
	}

	if !strings.HasPrefix(winPath, localDir) {
		debug("awatch: %s doesn't match our dir %s\n", winPath, localDir)
		return
	}

	runCmdsAndUpdateWindow()
}

func runCmdsAndUpdateWindow() {
	output := runCmds()
	httpApi.Put(fmt.Sprintf("/wins/%d/body", watchWin.Id), output)
}

func runCmds() (output *bytes.Buffer) {
	buf := new(bytes.Buffer)

	for _, c := range cmds {
		fmt.Fprintf(buf, "%% %s\n", c)
		debug("awatch: running command: %s\n", c)
		output, err := run(c)
		if err != nil {
			fmt.Fprintf(buf, "(execution error: %v)\n", err)
		}
		buf.Write(output)
	}

	return buf
}
