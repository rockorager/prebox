package keywork

type Backend interface {
	Name() string
	ListMailboxes() ([]Mailbox, error)
}
