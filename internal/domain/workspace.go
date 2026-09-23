package domain

type PreviewSession struct {
	URL       string `json:"url"`
	SessionID string `json:"sessionId"`
	Root      string `json:"-"`
	Active    bool   `json:"active"`
}

type PreviewRunner interface {
	Start(root string) (PreviewSession, error)
	Stop(root string) error
	Status(root string) PreviewSession
}

type Workspace interface {
	Root() string
	Activate(root string) error
}
