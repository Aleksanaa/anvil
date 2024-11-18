package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
)

func isWindowsPath(path string) bool {
	return len(path) >= 3 &&
		((path[0] >= 'A' && path[0] < 'Z') || (path[0] >= 'a' && path[0] <= 'z')) &&
		path[1] == ':' && path[2] == '\\'
}

type FileFinder struct {
	win *Window
}

func NewFileFinder(w *Window) *FileFinder {
	return &FileFinder{
		win: w,
	}
}

// Find looks for the file or directory `fpath` to make it a complete path. If path exists locally then `existsLocal` is
// set to true. If it is a remote path, it is not checked so as to not block the caller for the time needed to open an ssh connection
// for the first time.
func (f FileFinder) Find(fpath string) (fullpath *GlobalPath, existsLocal bool, err error) {
	var lfs localFs

	var gpath *GlobalPath
	gpath, err = NewGlobalPath(fpath, GlobalPathUnknown)
	if err != nil {
		return
	}

	winpath, err := f.winFile()
	if err != nil {
		return
	}
	log(LogCatgFS, "FileFinder.Find: window file is '%v'\n", winpath)

	if gpath.IsRemote() {
		// If it's a remote path then it's already complete. For example,
		// the relative ssh path host:dir/file is automatically treated as relative
		// to the home directory by the ssh server, so there is no need to complete it.
		fullpath = gpath
		return
	}

	if gpath.IsAbsolute() || (winpath.IsRemote() && path.IsAbs(fpath)) {
		log(LogCatgFS, "FileFinder.Find: path is absolute\n")
		existsLocal, err = lfs.fileExists(fpath)
		if err != nil {
			return
		}

		if existsLocal {
			log(LogCatgFS, "FileFinder.Find: path exists locally\n")
			fullpath, err = NewGlobalPath(fpath, GlobalPathUnknown)
			return
		}

		if winpath.IsRemote() && !gpath.IsRemote() {
			gpath = gpath.GlobalizeRelativeTo(winpath)
			log(LogCatgFS, "FileFinder.Find: window path is remote and requested path is not; using remote info from window path. Path is now: %s\n", gpath)
		}
		// window path is not remote but the path requested is absolute and doesn't exist locally.
		fullpath = gpath
		return
	}

	// path is relative
	fullpath = gpath.MakeAbsoluteRelativeTo(winpath)
	log(LogCatgFS, "FileFinder.Find: path is relative\n")
	log(LogCatgFS, "FileFinder.Find: full path combined with window path is %s\n", fullpath)
	if fullpath.IsRemote() {
		log(LogCatgFS, "FileFinder.Find: path combined with window path is remote\n")
		existsLocal = true
		return
	}

	existsLocal, err = lfs.fileExists(fullpath.String())
	log(LogCatgFS, "FileFinder.Find: path combined with window path exists: %v\n", existsLocal)
	if err != nil {
		return
	}

	return
}

func (f FileFinder) winFile() (path *GlobalPath, err error) {
	var lfs localFs
	rfs := NewSshFs(sshOptsFromSettings())

	path, err = f.winFileNoCheck()
	if err != nil {
		return
	}

	if path.dirState == GlobalPathUnknown {
		var isDir bool
		if path.IsRemote() {
			if f.win != nil && f.win.fileType != typeUnknown {
				// This saves needing to use the ssh connection to tell the filetype
				isDir = f.win.fileType == typeDir
			} else {
				isDir, err = rfs.isDir(path.String())
			}
		} else {
			isDir, err = lfs.isDir(path.String())
		}

		if isDir {
			path.SetDirState(GlobalPathIsDir)
		} else {
			path.SetDirState(GlobalPathIsFile)
		}
	}

	return
}

func (f FileFinder) winFileNoCheck() (path *GlobalPath, err error) {
	state := GlobalPathUnknown

	p := "."
	if f.win != nil {
		p = f.win.file
		if strings.HasSuffix(p, "+Errors") {
			p = p[:len(p)-7]
			state = GlobalPathIsDir
		}
		if strings.HasSuffix(p, "+Live") {
			p = p[:len(p)-5]
			state = GlobalPathIsDir
		}
		if f.win.fileType == typeDir {
			state = GlobalPathIsDir
		}
	} else {
		state = GlobalPathIsDir
	}

	path, err = NewGlobalPath(p, state)
	return
}

func (f FileFinder) WindowDir() (path string, err error) {
	winpath, err := f.winFileNoCheck()
	if err != nil {
		return
	}

	path = winpath.Dir().String()
	return
}

func (f FileFinder) WindowFile() (path string, err error) {
	winpath, err := f.winFileNoCheck()
	if err != nil {
		return
	}
	path = winpath.Path()
	return
}

type FileLoader struct {
}

func (l *FileLoader) Load(path string) (contents []byte, filenames []string, err error) {

	sfs, err := GetFs(path)

	isDir, err := sfs.isDir(path)
	if err != nil {
		return
	}

	if isDir {
		filenames, err = sfs.filenamesInDir(path)
	} else {
		contents, err = sfs.loadFile(path)
	}
	return
}

func (l *FileLoader) LoadAsync(path string) (load *DataLoad, err error) {
	sfs, err := GetFs(path)
	if err != nil {
		return
	}

	load = NewDataLoad()
	err = sfs.contentsAsync(path, load.Filenames, load.Contents, load.Errs, load.Kill)

	return
}

type DataLoad struct {
	Contents  chan []byte
	Filenames chan []string
	Errs      chan error // Will only contain one error
	Kill      chan struct{}
}

func NewDataLoad() *DataLoad {
	return &DataLoad{
		Errs:      make(chan error),
		Kill:      make(chan struct{}, 1),
		Contents:  make(chan []byte),
		Filenames: make(chan []string),
	}
}

// Save a local or remote file .
func (l *FileLoader) Save(path string, contents []byte) (err error) {

	sfs, err := GetFs(path)
	err = sfs.saveFile(path, contents)

	return
}

// SaveAsync asynchronously start writing `contents` to disk in the file `path`. If there is an error
// preparing to write to disk, `err` is set to non-nil. If writing to disk is started successfully,
// `save` can be used to track the progress of the write operation.
func (l *FileLoader) SaveAsync(path string, contents []byte) (save *DataSave, err error) {
	sfs, err := GetFs(path)
	if err != nil {
		return
	}

	save = NewDataSave()
	err = sfs.saveFileAsync(path, contents, save.Errs, save.Kill)

	return
}

// DataSave represents an asynchronous save operation in progress.
type DataSave struct {
	// Errs when read will contain an error if an error occurs during the
	// write operation. It is closed when the operation completes.
	Errs chan error // Will only contain one error
	// Kill may be written to kill the write operation
	Kill chan struct{}
}

func NewDataSave() *DataSave {
	return &DataSave{
		Errs: make(chan error),
		Kill: make(chan struct{}, 1),
	}
}

func GetFs(path string) (sfs simpleFs, err error) {
	// Local file or dir?
	isRemote, err := isRemoteFilenameOrDir(path)
	if err != nil {
		return
	}

	if isRemote {
		log(LogCatgFS, "GetFs: for %s, using ssh\n", path)
		r := NewSshFs(sshOptsFromSettings())
		sfs = r
	} else {
		log(LogCatgFS, "GetFs: for %s, using local filesystem\n", path)
		var l localFs
		sfs = l
	}

	return
}

func sshOptsFromSettings() sshFsOpts {
	return sshFsOpts{
		shell:      settings.Ssh.Shell,
		closeStdin: settings.Ssh.CloseStdin,
	}
}

type simpleFs interface {
	fileExists(path string) (ok bool, err error)
	isDir(path string) (ok bool, err error)
	isDirAsync(path string, kill chan struct{}) (ok bool, err error)
	loadFile(path string) (contents []byte, err error)
	loadFileAsync(path string, contents chan []byte, errs chan error, kill chan struct{}) (err error)
	saveFile(path string, contents []byte) (err error)
	saveFileAsync(path string, contents []byte, errs chan error, kill chan struct{}) (err error)
	filenamesInDir(path string) (names []string, err error)
	filenamesInDirAsync(path string, names chan []string, errs chan error, kill chan struct{}) (err error)
	exec(dir, cmd, arg string) (output []byte, err error)
	//execAsync(dir, cmd, arg string, stdin []byte, contents chan []byte, errs chan error, kill chan struct{}) (err error)
	execAsync(execCtx) (err error)
	contentsAsync(path string, names chan []string, contents chan []byte, errs chan error, kill chan struct{}) (err error)
}

type execCtx struct {
	dir         string
	cmd         string
	arg         string
	stdin       []byte
	contents    chan []byte
	errs        chan error
	kill        chan struct{}
	extraEnv    []string
	done        chan struct{}
	shellString string
}

func (c execCtx) fullEnv() []string {
	return append(os.Environ(), c.extraEnv...)
}

func (c execCtx) extraEnvNamesAndValues() (names, values []string, err error) {
	for _, e := range c.extraEnv {
		parts := strings.Split(e, "=")
		if len(parts) != 2 {
			err = fmt.Errorf("Invalid environment variable set: %s\n", e)
			return
		}
		names = append(names, parts[0])
		values = append(values, parts[1])
	}
	return
}

type localFs struct{}

func (f localFs) fileExists(path string) (ok bool, err error) {
	return fileExists(path)
}

func (f localFs) isDir(path string) (ok bool, err error) {
	return isDir(path)
}

func (f localFs) isDirAsync(path string, kill chan struct{}) (ok bool, err error) {
	return isDir(path)
}

func (f localFs) loadFile(path string) (contents []byte, err error) {
	return ioutil.ReadFile(path)
}

func (f localFs) loadFileAsync(path string, contents chan []byte, errs chan error, kill chan struct{}) (err error) {
	file, err := os.Open(path)
	if err != nil {
		return
	}

	go func() {
		copyBlocks(file, contents, 1024*1024, errs, kill)
		close(errs)
	}()
	return
}

func copyBlocks(source io.Reader, dest chan []byte, blocksize int, errs chan error, kill chan struct{}) {
	defer close(dest)

	count := 0
	updateBlockSize := func() {
		if blocksize >= 1048576 {
			return
		}

		if count < 50 {
			count++
			return
		}

		blocksize = 1048576
	}

	for {
		block := make([]byte, blocksize)
		if kill != nil {
			select {
			case <-kill:
				return
			default:
			}
		}

		n, err := source.Read(block)

		if err != nil {
			if err != io.EOF {
				// errs might already be closed, hence we send in a select statement
				select {
				case errs <- err:
				default:
				}
			}
			break
		}

		if n == 0 {
			continue
		}

		b := block
		if n < len(block) {
			b = block[:n]
		}
		dest <- b

		updateBlockSize()
	}
}

func (f localFs) saveFile(path string, contents []byte) (err error) {
	return ioutil.WriteFile(path, contents, 0664)
}

func (f localFs) saveFileAsync(path string, contents []byte, errs chan error, kill chan struct{}) (err error) {
	go func() {
		err := f.saveFile(path, contents)

		if err != nil {
			errs <- err
		}
		close(errs)
	}()
	return nil
}

func (f localFs) filenamesInDir(path string) (names []string, err error) {
	return filenamesInDir(path)
}

func (f localFs) filenamesInDirAsync(path string, names chan []string, errs chan error, kill chan struct{}) (err error) {
	// TODO: make this more asynchronous for huge directories
	go func() {
		lnames, err := filenamesInDir(path)
		if err != nil {
			errs <- err
			close(errs)
			return
		}

		names <- lnames
		close(names)
		close(errs)
	}()
	return
}

func (f localFs) contentsAsync(path string, names chan []string, contents chan []byte, errs chan error, kill chan struct{}) (err error) {
	isDir, err := f.isDir(path)
	if err != nil {
		return
	}

	if isDir {
		err = f.filenamesInDirAsync(path, names, errs, kill)
	} else {
		err = f.loadFileAsync(path, contents, errs, kill)
	}

	return
}

func (f localFs) exec(dir, command, arg string) (output []byte, err error) {
	var out bytes.Buffer
	args := fmt.Sprintf("%s %s", command, arg)
	cmd := exec.Command("bash", "-c", args)
	cmd.Stdout = &out
	cmd.Stderr = &out
	cmd.Dir = dir
	err = cmd.Run()
	output = out.Bytes()
	return
}

func (f localFs) execAsync(c execCtx) (err error) {
	cmd, stdout, stderr, closed, apiSess, err := f.setupForAsyncExec(c)
	if err != nil {
		return
	}

	err = cmd.Start()
	if err != nil {
		log(LogCatgFS, "localFs.execAsync: Start error: %v\n", err)
		stdout.Close()
		stderr.Close()
		if apiSess != nil {
			deleteApiSession(apiSess.Id())
		}
		return
	}

	go func() {
		_, ok := <-c.kill
		if ok {
			err := KillProcess(cmd.Process)
			if err != nil {
				log(LogCatgFS, "Error killing process: %v\n", err)
			}
		}
	}()

	go func() {
		// It seems that calling Wait too soon on a Command causes
		// an error like "read |0: file already closed". It seems to be recommended
		// to only call Wait after reading is finished.
		<-closed
		time.Sleep(200 * time.Millisecond)
		err := cmd.Wait()
		log(LogCatgFS, "localFs.execAsync: wait error: %v\n", err)
		if err != nil {
			log(LogCatgFS, "localFs.execAsync: sending error on chan\n")
			c.errs <- err
		}
		close(c.errs)
		/* Fix leak in goroutines */
		close(c.kill)
		if c.done != nil {
			close(c.done)
		}
		if apiSess != nil {
			deleteApiSession(apiSess.Id())
		}
	}()

	return
}

func (f localFs) setupForAsyncExec(c execCtx) (cmd *exec.Cmd, stdout, stderr io.ReadCloser, closed chan struct{}, apiSess *ApiSession, err error) {
	args := fmt.Sprintf("%s %s", c.cmd, c.arg)
	if runtime.GOOS == "windows" {
		cmd = WindowsCmd(args)
	} else {
		cmd = exec.Command("bash", "-c", args)
	}

	if c.stdin != nil {
		cmd.Stdin = bytes.NewBuffer(c.stdin)
	}

	stdout, err = cmd.StdoutPipe()
	if err != nil {
		return
	}

	stderr, err = cmd.StderrPipe()
	if err != nil {
		return
	}

	cmd.Dir = c.dir

	if c.extraEnv != nil {
		cmd.Env = c.fullEnv()
	}
	cmd.Env = append(cmd.Env, fmt.Sprintf("ANVIL_API_PORT=%d", LocalAPIPort()))

	apiSess, err = createApiSession(args)
	if err != nil {
		return
	}
	cmd.Env = append(cmd.Env, fmt.Sprintf("ANVIL_API_SESS=%s", apiSess.Id()))

	c3, closed := signalWhenComplete(c.contents)
	c1, c2 := mergeContentsInto(c3)

	go copyBlocks(stdout, c1, 1024*1024, c.errs, nil)
	go copyBlocks(stderr, c2, 1024*1024, c.errs, nil)

	return
}

func forkKill(kill chan struct{}) (kill2, kill3 chan struct{}) {
	kill2 = make(chan struct{})
	kill3 = make(chan struct{})

	go func() {
		for i := range kill {
			kill2 <- i
			kill3 <- i
		}
		close(kill2)
		close(kill3)
	}()
	return
}

func mergeContentsInto(dest chan []byte) (c1, c2 chan []byte) {
	c1 = make(chan []byte)
	c2 = make(chan []byte)

	go func() {
		var eofs [2]bool
		for !(eofs[0] && eofs[1]) {
			select {
			case b, ok := <-c1:
				if !ok {
					eofs[0] = true
					c1 = nil
					continue
				}
				dest <- b
			case b, ok := <-c2:
				if !ok {
					eofs[1] = true
					c2 = nil
					continue
				}
				dest <- b
			}
		}
		close(dest)
	}()
	return
}

// signalWhenComplete copies src to dest, and closes sig when src is closed
func signalWhenComplete(dest chan []byte) (src chan []byte, sig chan struct{}) {
	src = make(chan []byte)
	sig = make(chan struct{})

	go func() {
		for x := range src {
			dest <- x
		}
		close(sig)
		close(dest)
	}()

	return
}

func isRemoteFilenameOrDir(path string) (b bool, err error) {
	b, err = fileExists(path)
	if b && err == nil {
		return false, nil
	}

	var gp *GlobalPath
	gp, err = NewGlobalPath(path, GlobalPathUnknown)
	if err != nil {
		return false, err
	}

	b = gp.IsRemote()
	return
}

type sshFs struct {
	shell      string
	closeStdin bool
}

func NewSshFs(opts sshFsOpts) *sshFs {
	return &sshFs{
		shell:      opts.shell,
		closeStdin: opts.closeStdin,
	}
}

type sshFsOpts struct {
	shell      string
	closeStdin bool
}

func (f *sshFs) getShell() string {
	if f.shell == "" {
		return "sh"
	}
	return f.shell
}

func (f *sshFs) fileExists(path string) (ok bool, err error) {
	file, session, _, err := f.splitFilenameAndMakeSession(path, nil)
	if err != nil {
		return
	}
	defer session.Close()

	log(LogCatgFS, "sshFs.fileExists: checking for %s\n", path)

	cmd := fmt.Sprintf("%s -c 'if [ -e \"%s\" ]; then echo yes; else echo no; fi'", f.getShell(), file)
	log(LogCatgFS, "sshFs.fileExists: running command: %s\n", cmd)
	b, err := session.Output(cmd)
	if err != nil {
		return
	}
	log(LogCatgFS, "sshFs.fileExists: got output %s \n", string(b))

	s := string(b)
	if s == "yes\n" {
		ok = true
	}
	log(LogCatgFS, "sshFs.fileExists: returning %v,%v\n", ok, err)

	return
}

func (f *sshFs) isDirAsync(path string, kill chan struct{}) (ok bool, err error) {
	file, session, _, err := f.splitFilenameAndMakeSession(path, kill)
	if err != nil {
		return
	}
	defer session.Close()

	cmd := fmt.Sprintf("%s -c 'if [ -d \"%s\" ]; then echo yes; else echo no; fi'", f.getShell(), file)
	log(LogCatgFS, "sshFs.isDirAsync: running command: %s\n", cmd)
	b, err := session.Output(cmd)
	if err != nil {
		log(LogCatgFS, "sshFs.isDirAsync: got error: %v %T. Also got bytes: '%s'\n", err, err, string(b))
		return
	}
	log(LogCatgFS, "sshFs.isDirAsync: got output %s \n", string(b))

	s := string(b)
	if s == "yes\n" {
		ok = true
	}

	return
}

func (f *sshFs) isDir(path string) (ok bool, err error) {
	return f.isDirAsync(path, nil)
}

func (f *sshFs) splitFilenameAndMakeSession(path string, kill chan struct{}) (file string, session *ssh.Session, client *SshClient, err error) {
	client, file, err = f.splitFilenameAndDial(path, kill)
	if err != nil {
		return
	}

	session, err = client.NewSession()
	return
}

func (f *sshFs) splitFilenameAndDial(path string, kill chan struct{}) (client *SshClient, file string, err error) {
	gpath, err := NewGlobalPath(path, GlobalPathUnknown)
	if err != nil {
		return
	}

	log(LogCatgFS, "sshFs: split path %s into %#v\n", path, gpath)
	file = gpath.Path()

	endpt := SshEndpt{
		Dest: SshHop{
			User: gpath.User(),
			Host: gpath.Host(),
			Port: gpath.Port(),
		},
		Proxy: SshHop{
			User: gpath.ProxyUser(),
			Host: gpath.ProxyHost(),
			Port: gpath.ProxyPort(),
		},
	}
	client, err = f.dial(endpt, kill)
	return
}

func (f *sshFs) dial(endpt SshEndpt, kill chan struct{}) (client *SshClient, err error) {
	client, err = sshClientCache.Get(endpt, kill)
	log(LogCatgFS, "sshFs: retrieved ssh client from cache for %s. Error (if any)=%v\n", endpt, err)
	return
}

func (f *sshFs) loadFile(path string) (contents []byte, err error) {
	file, session, _, err := f.splitFilenameAndMakeSession(path, nil)
	if err != nil {
		return
	}
	defer session.Close()

	// sh -c 'if [ -d /tmp ]; then echo yes; else echo no; fi'
	cmd := fmt.Sprintf("%s -c 'cat \"%s\"'", f.getShell(), file)
	contents, err = session.Output(cmd)
	if err != nil {
		return
	}

	return
}

func (f *sshFs) loadFileAsync(path string, contents chan []byte, errs chan error, kill chan struct{}) (err error) {
	go func() {
		file, session, _, err := f.splitFilenameAndMakeSession(path, kill)
		if err != nil {
			errs <- err
			return
		}

		cmd := fmt.Sprintf("%s -c 'cat \"%s\"'", f.getShell(), file)
		// Ignore errors?
		//cmd := fmt.Sprintf("%s -c 'cat \"%s\" 2>/dev/null'", f.getShell(), file)

		stdout, err := session.StdoutPipe()
		if err != nil {
			errs <- err
			return
		}

		go func() {
			copyBlocks(stdout, contents, 4096, errs, kill)
			session.Close()
			close(errs)
		}()

		err = session.Start(cmd)
		if err != nil {
			errs <- err
			return
		}
	}()

	return nil
}

func (f *sshFs) saveFile(path string, contents []byte) (err error) {
	file, session, _, err := f.splitFilenameAndMakeSession(path, nil)
	if err != nil {
		return
	}
	defer session.Close()

	// sh -c 'if [ -d /tmp ]; then echo yes; else echo no; fi'
	cmd := fmt.Sprintf("%s -c 'cat > \"%s\"'", f.getShell(), file)
	pipe, err := session.StdinPipe()
	if err != nil {
		return
	}

	err = session.Start(cmd)
	if err != nil {
		return
	}

	_, err = pipe.Write(contents)
	if err != nil {
		return
	}

	pipe.Close()
	err = session.Wait()

	return
}

func (f sshFs) saveFileAsync(path string, contents []byte, errs chan error, kill chan struct{}) (err error) {
	//return fmt.Errorf("Not implemented yet")
	go func() {
		file, session, _, err := f.splitFilenameAndMakeSession(path, kill)
		if err != nil {
			errs <- err
			close(errs)
			return
		}

		cmd := fmt.Sprintf("%s -c 'cat > \"%s\"'", f.getShell(), file)
		log(LogCatgFS, "sshFs.saveFileAsync: running command: %s\n", cmd)

		pipe, err := session.StdinPipe()
		if err != nil {
			errs <- err
			close(errs)
			return
		}

		err = session.Start(cmd)
		if err != nil {
			errs <- err
			close(errs)
			return
		}

		go func() {
			_, ok := <-kill
			if !ok {
				return
			}
			session.Close()
			err := session.Wait()
			if err != nil {
				errs <- err
			}
			close(errs)
		}()

		_, err = pipe.Write(contents)
		if err != nil {
			errs <- err
			return
		}

		pipe.Close()
		err = session.Wait()
		if err != nil {
			errs <- err
			return
		}

		close(errs)
	}()

	return nil
}

func (f *sshFs) filenamesInDir(path string) (names []string, err error) {
	file, session, _, err := f.splitFilenameAndMakeSession(path, nil)
	if err != nil {
		return
	}
	defer session.Close()

	cmd := fmt.Sprintf("%s -c 'ls -Ap \"%s\" | cat'", f.getShell(), file)
	b, err := session.Output(cmd)
	if err != nil {
		return
	}

	names = strings.Split(string(b), "\n")

	return
}

func (f *sshFs) filenamesInDirAsync(path string, names chan []string, errs chan error, kill chan struct{}) (err error) {
	file, session, _, err := f.splitFilenameAndMakeSession(path, kill)
	if err != nil {
		return
	}

	// TODO: make this more asynchronous for huge directories
	go func() {
		cmd := fmt.Sprintf("%s -c 'ls -Ap \"%s\" | cat'", f.getShell(), file)
		b, err := session.Output(cmd)
		if err != nil {
			errs <- err
			close(names)
			close(errs)
			return
		}
		lnames := strings.Split(string(b), "\n")
		names <- lnames
		session.Close()
		close(names)
		close(errs)
	}()

	return
}

func (f *sshFs) contentsAsync(path string, names chan []string, contents chan []byte, errs chan error, kill chan struct{}) (err error) {
	go func() {
		isDir, err := f.isDirAsync(path, kill)
		if err != nil {
			errs <- err
			return
		}

		if isDir {
			err = f.filenamesInDirAsync(path, names, errs, kill)
		} else {
			err = f.loadFileAsync(path, contents, errs, kill)
		}

		if err != nil {
			errs <- err
			close(errs)
			return
		}
	}()

	return nil
}

func (f sshFs) exec(path, command, arg string) (output []byte, err error) {
	dir, session, _, err := f.splitFilenameAndMakeSession(path, nil)
	if err != nil {
		return
	}
	defer session.Close()

	cmd := fmt.Sprintf("%s -c 'cd \"%s\" && %s %s'", f.getShell(), dir, command, arg)
	log(LogCatgFS, "sshFs.exec: running command: %s\n", cmd)
	output, err = session.Output(cmd)
	return
}

func (f sshFs) execAsync(c execCtx) (err error) {
	go func() {
		session, cmd, apiSess, ok := f.setupForAsyncExec(c)
		if !ok {
			return
		}

		err = session.Start(cmd)
		if err != nil {
			c.errs <- err
			if apiSess != nil {
				deleteApiSession(apiSess.Id())
			}
			return
		}

		forceClosedSession := newSemchan()

		go func() {
			<-c.kill
			log(LogCatgFS, "sshFs.exec: kill received. Closing session\n")
			// See https://github.com/golang/go/issues/16597
			session.Signal(ssh.SIGKILL)
			go func() {
				time.Sleep(500 * time.Millisecond)
				forceClosedSession.write()
				session.Close()
			}()
		}()

		go func() {
			log(LogCatgFS, "sshFs.exec: kill: waiting for status\n")
			err := session.Wait()
			log(LogCatgFS, "sshFs.exec: kill: wait done\n")
			if err != nil {
				if forceClosedSession.wasWrittenTo() {
					err = fmt.Errorf("killed process, but got no response")
				}

				log(LogCatgFS, "sshFs.exec: wait error: %v\n", err)
				log(LogCatgFS, "sshFs.exec: sending error on chan\n")
				c.errs <- err
			}
			close(c.errs)
			if c.done != nil {
				close(c.done)
			}
			if apiSess != nil {
				deleteApiSession(apiSess.Id())
			}
		}()

	}()

	return
}

type semchan chan struct{}

func newSemchan() semchan {
	return make(chan struct{}, 1)
}

func (s semchan) write() {
	log(LogCatgFS, "semchan: write called\n")
	s <- struct{}{}
}

func (s semchan) wasWrittenTo() bool {
	select {
	case <-s:
		log(LogCatgFS, "semchan: wasWrittenTo: returning true\n")
		return true
	default:
	}
	log(LogCatgFS, "semchan: wasWrittenTo: returning false\n")
	return false
}

func (f sshFs) setupForAsyncExec(c execCtx) (session *ssh.Session, cmd string, apiSess *ApiSession, ok bool) {
	dir, session, client, err := f.splitFilenameAndMakeSession(c.dir, c.kill)
	if err != nil {
		c.errs <- err
		return
	}

	extra := ""
	if c.stdin == nil && f.closeStdin {
		extra = " 0<&-" // shell command to close stdin
	}

	cmd = buildShellString(c, f.getShell(), dir, extra)
	log(LogCatgFS, "sshFs.exec: running command: %s\n", cmd)

	if c.stdin != nil {
		session.Stdin = bytes.NewBuffer(c.stdin)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		c.errs <- err
		return
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		c.errs <- err
		return
	}

	// We start an API server on the listener if one is not started.
	// But each execution of a command gets a new session id
	err = f.maybeServeAPIOverSshClient(client)
	if err != nil {
		log(LogCatgFS, "%v\n", err)
	}

	apiSess, err = createApiSession(fmt.Sprintf("%s %s", c.cmd, c.arg))
	if err != nil {
		log(LogCatgFS, "%v\n", err)
	}
	session.Setenv("ANVIL_API_PORT", strconv.Itoa(client.ListenerPort()))
	session.Setenv("ANVIL_API_SESS", string(apiSess.Id()))

	if c.extraEnv != nil {
		names, values, err := c.extraEnvNamesAndValues()
		if err != nil {
			c.errs <- err
			if apiSess != nil {
				deleteApiSession(apiSess.Id())
			}
			return
		}

		for i, n := range names {
			log(LogCatgFS, "sshFs.execAsync: setting env var %s=%s\n", n, values[i])
			session.Setenv(n, values[i])
		}

	}

	c1, c2 := mergeContentsInto(c.contents)

	go copyBlocks(stdout, c1, 4096, c.errs, nil)
	go copyBlocks(stderr, c2, 4096, c.errs, nil)

	ok = true
	return
}

func buildShellString(c execCtx, shell, dir, extra string) string {
	log(LogCatgFS, "buildShellString: template is '%s'\n", c.shellString)
	if c.shellString == "" {
		return fmt.Sprintf(`%s -c $'cd "%s" && %s %s%s'`,
			shell, dir, escapeSingleTicks(c.cmd), escapeSingleTicks(c.arg), extra)
	}

	s := c.shellString
	s = strings.ReplaceAll(s, "{Dir}", dir)
	s = strings.ReplaceAll(s, "{Cmd}", escapeSingleTicks(c.cmd))
	s = strings.ReplaceAll(s, "{Args}", escapeSingleTicks(c.arg))
	return s
}

func (f sshFs) maybeServeAPIOverSshClient(client *SshClient) (err error) {

	if client.userData != nil {
		log(LogCatgFS, "sshFs.maybeServeAPIOverSshClient: API already started\n")
		// We already started the API on the client's listener
		return nil
	}

	listener, err := client.Listener()
	if err != nil {
		err = fmt.Errorf("sshFs.execAsync: listening on remote address failed: %v\n", err)
		return
	}

	log(LogCatgFS, "sshFs.maybeServeAPIOverSshClient: Serving API\n")
	go func() {
		err := ServeAPIOnListener(listener)
		if err != nil {
			log(LogCatgFS, "sshFs.execAsync: ServeAPIOnListener failed: %v\n", err)
		}
	}()

	client.userData = true

	return nil
}

var winInvalidPathSyntaxErr = syscall.Errno(123)

func fileExists(path string) (ok bool, err error) {

	if _, err = os.Stat(path); err == nil {
		ok = true
	} else if errors.Is(err, fs.ErrNotExist) {
		ok = false
		err = nil
	} else if errors.Is(err, winInvalidPathSyntaxErr) {
		// On Windows if we try and Stat a unix path it causes the error "CreateFile host:/path/to/something/: The filename, directory name, or volume label syntax is incorrect."
		// This is returned as a *fs.PathError, who's Err field is set to a syscall.Errno, which has the error number 123. So
		// we check for that here.
		// When this happens we know the path is not a valid local path.
		ok = false
		err = nil
	}
	return
}

func isDir(path string) (ok bool, err error) {
	if s, err := os.Stat(path); err == nil && s.IsDir() {
		ok = true
	}
	return
}

func filenamesInDir(path string) (names []string, err error) {

	entries, err := os.ReadDir(path)
	if err != nil {
		return
	}

	names = make([]string, 0, len(entries))
	for _, e := range entries {
		n := e.Name()
		if n == "." || n == ".." {
			continue
		}

		fi, err := os.Stat(filepath.Join(path, e.Name()))
		if err == nil && fi.IsDir() {
			n = fmt.Sprintf("%s%c", n, filepath.Separator)
		}

		names = append(names, n)
	}

	return
}

func pathIsRemote(path string) (bool, error) {
	p, err := NewGlobalPath(path, GlobalPathUnknown)
	if err != nil {
		return false, err
	}

	return p.IsRemote(), nil
}

func escapeSingleTicks(s string) string {
	return strings.ReplaceAll(s, "'", "\\'")
}
