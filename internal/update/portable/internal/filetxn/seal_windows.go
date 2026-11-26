package filetxn

import (
	"os"
	"sidravia/internal/update/transaction"
)

func RemoveSeal(target, id string) error {
	root, err := os.OpenRoot(target)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = transaction.VerifyPortableSeal(target, id); err != nil {
		return err
	}
	return root.Remove(transaction.StartupSeal)
}
