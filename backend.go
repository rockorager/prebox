package prebox

type Backend interface {
	Name() string
	ListMailboxes() ([]Mailbox, error)
	Search(limit int, offset int, query []string) (total uint64, result []Email, err error)
	AddConnection(*Connection)
	RemoveConnection(*Connection)
}
