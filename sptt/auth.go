package sptt

import (
	"crypto/rand"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/hex"
)

// dummySalt and dummySecret are pre-computed at init time and used when an
// auth token name is not found, so that the verification code path — and
// therefore its timing — is identical whether or not the name exists.
var (
	dummySalt   []byte
	dummySecret []byte
)

func init() {
	salt := make([]byte, 16)
	token := make([]byte, 64)
	if _, err := rand.Read(salt); err != nil {
		panic("sptt/auth: failed to generate dummy salt: " + err.Error())
	}
	if _, err := rand.Read(token); err != nil {
		panic("sptt/auth: failed to generate dummy token: " + err.Error())
	}
	dummySalt = salt
	h := sha512.New()
	h.Write(salt)
	h.Write(token)
	dummySecret = h.Sum(nil)
}

// GenerateToken creates a new auth token for the given name and clearance.
// It returns the raw token hex string (128 hex chars) that must be returned
// to the caller — it is never stored and cannot be retrieved again.
// The caller is responsible for persisting name, saltHex, secretHex, clearance
// via DB.CreateAuthToken.
func GenerateToken() (tokenHex, saltHex, secretHex string, err error) {
	rawToken := make([]byte, 64) // 512 bits
	rawSalt := make([]byte, 16)  // 128 bits

	if _, err = rand.Read(rawToken); err != nil {
		return
	}
	if _, err = rand.Read(rawSalt); err != nil {
		return
	}

	h := sha512.New()
	h.Write(rawSalt)
	h.Write(rawToken)
	digest := h.Sum(nil)

	tokenHex = hex.EncodeToString(rawToken)
	saltHex = hex.EncodeToString(rawSalt)
	secretHex = hex.EncodeToString(digest)
	return
}

// Authenticate verifies a token against the database.
// It returns (clearance, true) on success or (0, false) on any failure.
// The reason for failure (name not found vs. wrong token) is never disclosed.
func Authenticate(db *DB, name, tokenHex string) (int, bool) {
	row, err := db.GetAuthToken(name)

	var saltBytes, secretBytes []byte

	if err != nil {
		// Name not found — use dummy values to keep timing constant.
		saltBytes = dummySalt
		secretBytes = dummySecret
	} else {
		saltBytes, err = hex.DecodeString(row.Salt)
		if err != nil {
			saltBytes = dummySalt
			secretBytes = dummySecret
		} else {
			secretBytes, err = hex.DecodeString(row.Secret)
			if err != nil {
				saltBytes = dummySalt
				secretBytes = dummySecret
			}
		}
	}

	providedBytes, decErr := hex.DecodeString(tokenHex)
	if decErr != nil {
		// Still run the hash so timing is consistent.
		providedBytes = make([]byte, 64)
	}

	h := sha512.New()
	h.Write(saltBytes)
	h.Write(providedBytes)
	computed := h.Sum(nil)

	// Always constant-time compare — never short-circuit.
	match := subtle.ConstantTimeCompare(computed, secretBytes) == 1

	if err != nil || decErr != nil || !match {
		return 0, false
	}
	return row.Clearance, true
}
