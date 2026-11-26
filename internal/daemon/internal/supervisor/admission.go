package supervisor

import (
	"fmt"

	"sidravia/internal/daemon/internal/session"
)

func (s *Supervisor) reserveDeletion(id ID, ms *managedSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.sessions[id]
	if !ok || current != ms {
		return fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	ms.reserved = true
	return nil
}

func (s *Supervisor) reserveAdmission(id ID, ms *managedSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.sessions[id]
	if !ok || current != ms {
		return fmt.Errorf("%w: %q", ErrSessionNotFound, id)
	}
	if ms.intent == session.MaintainAuthentication {
		for otherID, other := range s.sessions {
			if otherID != id && other.intent == session.MaintainAuthentication &&
				(other.state != stateStopped || other.reserved) {
				return fmt.Errorf("%w", ErrActiveSessionConflict)
			}
		}
	}
	ms.reserved = true
	return nil
}

func (s *Supervisor) releaseReservation(id ID, ms *managedSession) {
	s.mu.Lock()
	if current, ok := s.sessions[id]; ok && current == ms {
		ms.reserved = false
	}
	s.mu.Unlock()
}

func (s *Supervisor) markActive(id ID, ms *managedSession) {
	s.mu.Lock()
	if current, ok := s.sessions[id]; ok && current == ms {
		ms.state = stateActive
	}
	s.mu.Unlock()
}
