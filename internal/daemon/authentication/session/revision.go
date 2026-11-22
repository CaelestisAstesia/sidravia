package session

import (
	"reflect"
	"time"
)

func (session *AuthenticationSession) updateSnapshot(mutate func(*Snapshot)) {
	before := session.currentSnapshot.Clone()
	mutate(&session.currentSnapshot)
	if publicSnapshotsEqual(before, session.currentSnapshot) {
		return
	}
	session.currentSnapshot.Revision++
	session.currentSnapshot.UpdatedAt = session.now()
	session.publishRevision()
}

func publicSnapshotsEqual(left, right Snapshot) bool {
	left.Revision, right.Revision = 0, 0
	left.UpdatedAt, right.UpdatedAt = time.Time{}, time.Time{}
	return reflect.DeepEqual(left, right)
}

func (session *AuthenticationSession) publishInitialRevision() { session.publishRevision() }

func (session *AuthenticationSession) publishRevision() {
	event := RevisionEvent{AuthenticationSessionID: session.currentSnapshot.AuthenticationSessionID, Revision: session.currentSnapshot.Revision, Snapshot: session.currentSnapshot.Clone()}
	select {
	case session.revisions <- event:
	default:
		select {
		case <-session.revisions:
		default:
		}
		select {
		case session.revisions <- event:
		default:
		}
	}
	// The diagnostic sink observes every committed revision directly, without
	// going through the coalescing RevisionEvents stream, so no intermediate
	// public revision is lost. It has no return value and cannot change state.
	session.diagnostics.SessionSnapshot(session.currentSnapshot.Clone())
}
