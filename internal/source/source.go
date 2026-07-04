package source

import "github.com/ashep/gnucashsync/internal/model"

type Source interface {
	Transactions() ([]model.Transaction, error)
}
