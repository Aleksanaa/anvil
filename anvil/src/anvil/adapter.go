package main

import (
	"fmt"
	"gioui.org/layout"
)

// adapter is the interface between the editable and the environment it is
// embedded in (the editor)
type adapter interface {
	completeFilename(word string, callback CompletionsCallback)
	appendError(dir, msg string)
	copyAllSelectionsFromLastSelectedEditable(gtx layout.Context)
	cutAllSelectionsFromLastSelectedEditable(gtx layout.Context)
	textOfAllSelectionsInLastSelectedEditable() []string
	pasteToFocusedEditable(gtx layout.Context)
	execute(e *editable, gtx layout.Context, cmd string, args []string)
	plumb(e *editable, gtx layout.Context, obj string) (plumbed bool)
	loadFileAndGoto(gtx layout.Context, path string, opts LoadFileOpts)
	loadFile(gtx layout.Context, path string)
	loadFileInPlaceAndGoto(gtx layout.Context, path string, opts LoadFileOpts)
	loadFileInPlace(gtx layout.Context, path string)
	textOfLastSelectionInEditor() string
	shiftEditorItemsDueToTextModification(startOfChange, lengthOfChange int)
	setFocusedEditable(e *editable)
	focusedEditable() *editable
	findFile(file string) (path *GlobalPath, err error)
	dir() string
	put()
	get()
	file() string
	mark(markName, file string, cursorIndex int)
	gotoMark(markName string)
	doWork(w Work)
	addJob(j Job)
	replaceCrWithTofu() bool
	setShellString(s string)
	addOpForNextLayout(op LayoutOp)
	setEditableWhereTertiaryButtonHoldStarted(ed *editable)
	getEditableWhereTertiaryButtonHoldStarted() *editable
	clearEditableWhereTertiaryButtonHoldStarted()
	style() Style
	setStyle(s Style)
	insertWhenTabPressed() string
}

// editableAdapter connects an editable with the rest of the editor (it's owning window, etc)
// so that it has less dependencies
type editableAdapter struct {
	fileFinder *FileFinder
	executor   *CommandExecutor
	// owner is the owner of the editable: a Window, Col or Editor.
	owner       interface{}
	shellString string
}

func (a editableAdapter) completeFilename(word string, callback CompletionsCallback) {
	dir, base, err := computeDirAndBaseForFilenameCompletion(word, a.fileFinder)
	log(LogCatgCompletion, "adapter: Complete on dir='%s' base='%s'\n", dir, base)

	// This will call editable.applyFilenameCompletions when complete
	err = FilenameCompletionsAsync(word, dir, base, callback)
	if err != nil {
		editor.AppendError(dir, err.Error())
		return
	}
}

func (a editableAdapter) appendError(dir, msg string) {
	editor.AppendError(dir, msg)
}

func (a editableAdapter) copyAllSelectionsFromLastSelectedEditable(gtx layout.Context) {
	editor.copyAllSelectionsFromLastSelectedEditable(gtx)
}

func (a editableAdapter) cutAllSelectionsFromLastSelectedEditable(gtx layout.Context) {
	editor.cutAllSelectionsFromLastSelectedEditable(gtx)
}

func (a editableAdapter) pasteToFocusedEditable(gtx layout.Context) {
	editor.pasteToFocusedEditable(gtx)
}

func (a editableAdapter) execute(e *editable, gtx layout.Context, cmd string, args []string) {
	if args == nil {
		args = []string{}
	}

	log(LogCatgCmd, "adapter: Execute '%s' %v\n", cmd, args)
	if a.executor != nil {
		ctx := a.buildCmdContext(e, gtx, args)
		ctx.RawCommand = cmd
		a.executor.Do(cmd, ctx)
	}
}

func (a editableAdapter) dir() string {
	dir, err := a.fileFinder.WindowDir()
	if err != nil {
		dir = ""
	}
	return dir
}

func (a editableAdapter) findFile(file string) (path *GlobalPath, err error) {
	d, err2 := a.fileFinder.WindowDir()
	if err2 != nil {
		d = ""
	}

	path, _, err = a.fileFinder.Find(file)
	if err != nil {
		a.appendError(d, err.Error())
		path = nil
	}

	return
}

func (a editableAdapter) buildCmdContext(e *editable, gtx layout.Context, args []string) *CmdContext {
	dir, err := a.fileFinder.WindowDir()
	if err != nil {
		dir = ""
	}

	return &CmdContext{Gtx: gtx,
		Dir:         dir,
		Editable:    e.executeOn,
		Args:        args,
		Selections:  e.selections,
		ShellString: a.shellString,
	}
}
func (a *editableAdapter) setShellString(s string) {
	a.shellString = s
}

func (a editableAdapter) plumb(e *editable, gtx layout.Context, obj string) (plumbed bool) {
	if plumber != nil && a.executor != nil {
		ctx := a.buildCmdContext(e, gtx, nil)
		var err error
		plumbed, err = plumber.Plumb(obj, a.executor, ctx)
		if err != nil {
			log(LogCatgPlumb, "adapter: Error plumbing: %v\n", err)
		}
	}
	return
}

func (a editableAdapter) column() *Col {
	var col *Col

	switch v := a.owner.(type) {
	case Window:
	case *Window:
		col = v.col
	case Col:
		col = &v
	case *Col:
		col = v
	}

	return col
}

func (a editableAdapter) loadFileAndGoto(gtx layout.Context, path string, opts LoadFileOpts) {
	opts.InCol = a.column()
	w := editor.LoadFileOpts(path, opts)
	if w != nil {
		w.SetFocus(gtx)
	}
}

func (a editableAdapter) loadFile(gtx layout.Context, path string) {
	var opts LoadFileOpts
	opts.InCol = a.column()
	w := editor.LoadFileOpts(path, opts)
	if w != nil {
		w.SetFocus(gtx)
	}
}

func (a editableAdapter) loadFileInPlaceAndGoto(gtx layout.Context, path string, opts LoadFileOpts) {
	win, ok := a.owner.(*Window)
	if !ok {
		return
	}

	err := win.LoadFileAndGoto(path, opts.GoTo, opts.SelectBehaviour, opts.GrowBodyBehaviour)
	if err != nil {
		log(LogCatgWin, "adapter: Loading file into window failed: %v\n", err)
	}
}

func (a editableAdapter) loadFileInPlace(gtx layout.Context, path string) {
	win, ok := a.owner.(*Window)
	if !ok {
		return
	}

	err := win.LoadFile(path)
	if err != nil {
		log(LogCatgWin, "adapter: Loading file into window failed: %v\n", err)
	}
}

func (a editableAdapter) textOfLastSelectionInEditor() string {
	sel := editor.lastSelection
	if sel.isSet && sel.editable != nil {
		return sel.editable.textOfSelection(sel.sel)
	}
	return ""
}

func (a editableAdapter) textOfAllSelectionsInLastSelectedEditable() []string {
	sel := editor.lastSelection
	ed := sel.editable
	if !sel.isSet || ed == nil {
		return nil
	}

	res := []string{}
	for _, s := range ed.selections {
		res = append(res, ed.textOfSelection(s))
	}
	return res
}

func (a editableAdapter) shiftEditorItemsDueToTextModification(startOfChange, lengthOfChange int) {
	if file, err := a.fileFinder.WindowFile(); err == nil {
		editor.Marks.ShiftDueToTextModification(file, startOfChange, lengthOfChange)
	}
}

func (a editableAdapter) setFocusedEditable(e *editable) {
	w := (*Window)(nil)
	if win, ok := a.owner.(*Window); ok {
		w = win
	}

	editor.setFocusedEditable(e, w)
}

func (a editableAdapter) focusedEditable() *editable {
	return editor.getFocusedEditable()
}

func (a editableAdapter) put() {
	w, ok := a.owner.(*Window)
	if ok {
		w.Put()
	}
}

func (a editableAdapter) get() {
	w, ok := a.owner.(*Window)
	if ok {
		w.Get()
	}
}

func (a editableAdapter) file() string {
	file := ""
	w, ok := a.owner.(*Window)
	if ok {
		file = w.file
	}
	return file
}

func (a editableAdapter) mark(markName, file string, cursorIndex int) {
	editor.Marks.Set(markName, file, cursorIndex)
}

func (a editableAdapter) gotoMark(markName string) {
	file, seek, ok := editor.Marks.Seek(markName)
	if ok {
		editor.LoadFileOpts(file, LoadFileOpts{GoTo: seek, SelectBehaviour: dontSelectText})
	}
}

func (a editableAdapter) doWork(w Work) {
	editor.WorkChan() <- w
}

func (a editableAdapter) addJob(j Job) {
	editor.AddJob(j)
}

func (a editableAdapter) replaceCrWithTofu() bool {
	return settings.Typesetting.ReplaceCRWithTofu
}

func (a editableAdapter) addOpForNextLayout(op LayoutOp) {
	editor.AddOpForNextLayout(op)
}

func (a editableAdapter) setEditableWhereTertiaryButtonHoldStarted(ed *editable) {
	editor.setEditableWhereTertiaryButtonHoldStarted(ed)
}

func (a editableAdapter) getEditableWhereTertiaryButtonHoldStarted() *editable {
	return editor.getEditableWhereTertiaryButtonHoldStarted()
}

func (a editableAdapter) clearEditableWhereTertiaryButtonHoldStarted() {
	editor.clearEditableWhereTertiaryButtonHoldStarted()
}

func (a editableAdapter) style() Style {
	return WindowStyle
}

func (a editableAdapter) setStyle(s Style) {
	WindowStyle = s
	editor.SetStyle(s)
}

func (a editableAdapter) insertWhenTabPressed() string {
	win, ok := a.owner.(*Window)
	if !ok {
		return editor.getInsertWhenTabPressed()
	}

	s := win.getInsertWhenTabPressed()
	if s == "" {
		s = editor.getInsertWhenTabPressed()
	}
	return s
}

type nilAdapter struct{}

func (a nilAdapter) completeFilename(word string, callback CompletionsCallback)         {}
func (a nilAdapter) appendError(dir, msg string)                                        {}
func (a nilAdapter) copyAllSelectionsFromLastSelectedEditable(gtx layout.Context)       {}
func (a nilAdapter) cutAllSelectionsFromLastSelectedEditable(gtx layout.Context)        {}
func (a nilAdapter) textOfAllSelectionsInLastSelectedEditable() []string                { return nil }
func (a nilAdapter) pasteToFocusedEditable(gtx layout.Context)                          {}
func (a nilAdapter) execute(e *editable, gtx layout.Context, cmd string, args []string) {}
func (a nilAdapter) plumb(e *editable, gtx layout.Context, obj string) (plumbed bool)   { return false }
func (a nilAdapter) loadFileAndGoto(gtx layout.Context, path string, opts LoadFileOpts) {
}
func (a nilAdapter) loadFile(gtx layout.Context, path string)                                {}
func (a nilAdapter) textOfLastSelectionInEditor() string                                     { return "" }
func (a nilAdapter) shiftEditorItemsDueToTextModification(startOfChange, lengthOfChange int) {}
func (a nilAdapter) setFocusedEditable(e *editable)                                          {}
func (a nilAdapter) focusedEditable() *editable                                              { return nil }
func (a nilAdapter) findFile(file string) (path *GlobalPath, err error) {
	return nil, fmt.Errorf("not implemented")
}
func (a nilAdapter) dir() string                                                               { return "" }
func (a nilAdapter) put()                                                                      {}
func (a nilAdapter) get()                                                                      {}
func (a nilAdapter) file() string                                                              { return "" }
func (a nilAdapter) mark(markName, file string, cursorIndex int)                               {}
func (a nilAdapter) gotoMark(markName string)                                                  {}
func (a nilAdapter) doWork(w Work)                                                             {}
func (a nilAdapter) loadFileInPlaceAndGoto(gtx layout.Context, path string, opts LoadFileOpts) {}
func (a nilAdapter) loadFileInPlace(gtx layout.Context, path string)                           {}
func (a nilAdapter) replaceCrWithTofu() bool                                                   { return false }
func (a nilAdapter) setShellString(s string)                                                   {}
func (a nilAdapter) addOpForNextLayout(op LayoutOp)                                            {}
func (a nilAdapter) addJob(j Job)                                                              {}
func (a nilAdapter) setEditableWhereTertiaryButtonHoldStarted(ed *editable)                    {}
func (a nilAdapter) getEditableWhereTertiaryButtonHoldStarted() *editable                      { return nil }
func (a nilAdapter) clearEditableWhereTertiaryButtonHoldStarted()                              {}
func (a nilAdapter) style() Style                                                              { return Style{} }
func (a nilAdapter) setStyle(s Style)                                                          {}
func (a nilAdapter) insertWhenTabPressed() string                                              { return "\t" }
