package mpc

import (
	"encoding/base64"
	"fmt"

	session "github.com/vultisig/go-wrappers/go-dkls/sessions"
	eddsaSession "github.com/vultisig/go-wrappers/go-schnorr/sessions"
	vgcommon "github.com/vultisig/vultisig-go/common"
)

type Handle int32

type Wrapper struct {
	isEdDSA bool
}

func NewWrapper(isEdDSA bool) *Wrapper {
	return &Wrapper{isEdDSA: isEdDSA}
}

func (w *Wrapper) QcSetupMsgNew(keyshareHandle Handle, threshold int, ids []string, oldParties []int, newParties []int) ([]byte, error) {
	if w.isEdDSA {
		return eddsaSession.SchnorrQcSetupMsgNew(eddsaSession.Handle(keyshareHandle), threshold, ids, oldParties, newParties)
	}
	return session.DklsQcSetupMsgNew(session.Handle(keyshareHandle), threshold, ids, oldParties, newParties)
}

func (w *Wrapper) QcSessionFromSetup(setupMsg []byte, id string, keyshareHandle Handle) (Handle, error) {
	if w.isEdDSA {
		h, err := eddsaSession.SchnorrQcSessionFromSetup(setupMsg, id, eddsaSession.Handle(keyshareHandle))
		return Handle(h), err
	}
	h, err := session.DklsQcSessionFromSetup(setupMsg, id, session.Handle(keyshareHandle))
	return Handle(h), err
}

func (w *Wrapper) QcSessionOutputMessage(h Handle) ([]byte, error) {
	if w.isEdDSA {
		return eddsaSession.SchnorrQcSessionOutputMessage(eddsaSession.Handle(h))
	}
	return session.DklsQcSessionOutputMessage(session.Handle(h))
}

func (w *Wrapper) QcSessionMessageReceiver(h Handle, message []byte, index int) (string, error) {
	if w.isEdDSA {
		return eddsaSession.SchnorrQcSessionMessageReceiver(eddsaSession.Handle(h), message, index)
	}
	return session.DklsQcSessionMessageReceiver(session.Handle(h), message, index)
}

func (w *Wrapper) QcSessionInputMessage(h Handle, message []byte) (bool, error) {
	if w.isEdDSA {
		return eddsaSession.SchnorrQcSessionInputMessage(eddsaSession.Handle(h), message)
	}
	return session.DklsQcSessionInputMessage(session.Handle(h), message)
}

// QcSessionFinish completes the QC session and returns a keyshare handle.
// The returned handle must be freed with KeyshareFree when no longer needed.
// There is no separate QcSessionFree in go-wrappers — the QC session is
// consumed by Finish and its resources released through the resulting keyshare.
func (w *Wrapper) QcSessionFinish(h Handle) (Handle, error) {
	if w.isEdDSA {
		result, err := eddsaSession.SchnorrQcSessionFinish(eddsaSession.Handle(h))
		return Handle(result), err
	}
	result, err := session.DklsQcSessionFinish(session.Handle(h))
	return Handle(result), err
}

func (w *Wrapper) KeyshareFromBytes(buf []byte) (Handle, error) {
	if w.isEdDSA {
		h, err := eddsaSession.SchnorrKeyshareFromBytes(buf)
		return Handle(h), err
	}
	h, err := session.DklsKeyshareFromBytes(buf)
	return Handle(h), err
}

func (w *Wrapper) KeyshareToBytes(share Handle) ([]byte, error) {
	if w.isEdDSA {
		return eddsaSession.SchnorrKeyshareToBytes(eddsaSession.Handle(share))
	}
	return session.DklsKeyshareToBytes(session.Handle(share))
}

func (w *Wrapper) KeysharePublicKey(share Handle) ([]byte, error) {
	if w.isEdDSA {
		return eddsaSession.SchnorrKeysharePublicKey(eddsaSession.Handle(share))
	}
	return session.DklsKeysharePublicKey(session.Handle(share))
}

// KeyshareChainCode returns the chain code for ECDSA keyshares.
// EdDSA (Schnorr) keyshares do not have chain codes — returns empty slice.
func (w *Wrapper) KeyshareChainCode(share Handle) ([]byte, error) {
	if w.isEdDSA {
		return []byte{}, nil
	}
	return session.DklsKeyshareChainCode(session.Handle(share))
}

// KeyshareFree releases native memory for a keyshare handle.
// For ECDSA: calls DklsKeyshareFree.
// For EdDSA: go-wrappers (go-schnorr) does not expose SchnorrKeyshareFree;
// Schnorr keyshare handles are managed internally by the library.
func (w *Wrapper) KeyshareFree(share Handle) error {
	if w.isEdDSA {
		return nil
	}
	return session.DklsKeyshareFree(session.Handle(share))
}

// DecodeDecryptMessage processes an inbound relay message.
// Wire format: base64( gcm_encrypt( base64(raw_payload) ) )
// Steps: base64 decode → AES-GCM decrypt → base64 decode → raw bytes
func DecodeDecryptMessage(body string, hexEncryptionKey string) ([]byte, error) {
	decodedBody, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode relay message: %w", err)
	}
	rawBody, err := vgcommon.DecryptGCM(decodedBody, hexEncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to AES-GCM decrypt relay message: %w", err)
	}
	inboundBody, err := base64.StdEncoding.DecodeString(string(rawBody))
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode inner payload: %w", err)
	}
	return inboundBody, nil
}

// EncryptEncodeSetupMessage prepares a setup message for relay upload.
// Wire format: base64( gcm_encrypt( base64(raw_setup_bytes) ) )
// This is the inverse of DecodeDecryptMessage.
func EncryptEncodeSetupMessage(setupMsg []byte, hexEncryptionKey string) (string, error) {
	innerB64 := base64.StdEncoding.EncodeToString(setupMsg)
	encrypted, err := vgcommon.EncryptGCM(innerB64, hexEncryptionKey)
	if err != nil {
		return "", fmt.Errorf("failed to AES-GCM encrypt setup message: %w", err)
	}
	return encrypted, nil
}
