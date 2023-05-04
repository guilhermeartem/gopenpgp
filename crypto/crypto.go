package crypto

import (
	"github.com/ProtonMail/gopenpgp/v3/constants"
	"github.com/ProtonMail/gopenpgp/v3/profile"
)

type PGPHandle struct {
	profile profile.Profile
}

// PGP creates a PGPHandle to interact with the API.
// Uses the default profile for configuration.
func PGP() *PGPHandle {
	return PGPWithProfile(profile.Default())
}

// PGPCryptoRefresh creates a PGPHandle to interact with the API.
// Uses the the new crypto refresh profile.
func PGPCryptoRefresh() *PGPHandle {
	return PGPWithProfile(profile.CryptoRefresh())
}

// PGPWithProfile creates a PGPHandle to interact with the API.
// Uses the provided profile for configuration.
func PGPWithProfile(profile profile.Profile) *PGPHandle {
	return &PGPHandle{
		profile: profile,
	}
}

// Decryption returns a builder to create a DecryptionHandle
// for decrypting pgp messages.
func (p *PGPHandle) Decryption() DecryptionHandleBuilder {
	return newDecryptionHandleBuilder()
}

// Encryption returns a builder to create an EncryptionHandle
// for encrypting messages.
func (p *PGPHandle) Encryption() EncryptionHandleBuilder {
	return newEncryptionHandleBuilder(p.profile)
}

// Sign returns a builder to create a SignHandle
// for signing messages.
func (p *PGPHandle) Sign() SignatureHandleBuilder {
	return newSignatureHandleBuilder(p.profile)
}

// Verify returns a builder to create an VerifyHandle
// for verifying signatures.
func (p *PGPHandle) Verify() VerifyHandleBuilder {
	return newVerifyHandleBuilder()
}

// LockKey encrypts the private parts of a copy of the input key with the given passphrase.
func (p *PGPHandle) LockKey(key *Key, passphrase []byte) (*Key, error) {
	return key.lock(passphrase, p.profile.KeyEncryptionConfig())
}

// GenerateKey generates key according to the current profile.
// The argument level allows to set the security level, either standard or high.
// The profile defines the algorithms and parameters that are used for each security level.
func (p *PGPHandle) GenerateKey(name, email string, level constants.SecurityLevel) (*Key, error) {
	return generateKey(name, email, p.localTime, p.profile, level)
}

// GenerateSessionKey generates a random key for the default cipher.
func (p *PGPHandle) GenerateSessionKey() (*SessionKey, error) {
	return generateSessionKey(p.profile.EncryptionConfig())
}