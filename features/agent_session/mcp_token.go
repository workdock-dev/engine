package agent_session

import (
	"crypto/rand"
	"encoding/hex"
)

func newMCPToken() (string, error) {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(token[:]), nil
}
