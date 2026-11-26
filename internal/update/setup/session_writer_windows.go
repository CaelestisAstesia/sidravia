package setup

import (
	"context"
	"encoding/json"
	"os"

	"sidravia/internal/document/securefile"
	"sidravia/internal/update/setup/internal/receiptstate"
)

func saveSession(ctx context.Context, s receiptstate.Session) error {
	path, e := receiptstate.Path(s.Source.ID, s.OperationID)
	if e != nil {
		return e
	}
	if s.SchemaVersion != 1 || s.Source.Layout != "installed" || s.Source.Validate() != nil || s.Updater.PID == 0 || s.Updater.Created == 0 {
		return ErrInvalid
	}
	store, e := securefile.NewSecureStore(securefile.SecureStoreOptions{})
	if e != nil {
		return e
	}
	b, e := json.Marshal(s)
	if e != nil {
		return e
	}
	return store.Replace(ctx, path, b)
}
func claimFinalization(target, id string) error {
	path, err := receiptstate.Path(target, id)
	if err != nil {
		return err
	}
	claim, err := os.OpenFile(path+".finish", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	return claim.Close()
}
