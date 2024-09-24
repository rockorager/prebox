package prebox

type Backend interface {
	Name() string
	ListMailboxes() ([]Mailbox, error)
	Search([]string) ([]Email, error)
	AddConnection(*Connection)
	RemoveConnection(*Connection)
}
