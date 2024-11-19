package api

type Window struct {
	Id         int
	GlobalPath string
	Path       string
}

type WindowBody struct {
	Len int
}

type Notification struct {
	WinId  int
	Op     NotificationOp
	Offset int
	Len    int
	Cmd    []string
}

type Selection struct {
	Start, End, Len int
}

type NotificationOp int

const (
	NotificationOpInsert = iota
	NotificationOpDelete
	NotificationOpExec
	NotificationOpPut
	NotificationOpFileClosed
	NotificationOpFileOpened
)

type ExecuteReq struct {
	Cmd  string
	Args []string
}

type WebsockMessageId int

const (
	WebsockMessageNotification = iota
)
