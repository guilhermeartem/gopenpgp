package crypto

import (
	"io"
	"time"

	"github.com/ProtonMail/go-crypto/v2/openpgp"
	"github.com/ProtonMail/go-crypto/v2/openpgp/packet"
	"github.com/pkg/errors"
)

type pgpSplitWriter struct {
	keyPackets        Writer
	ciphertext        Writer
	detachedSignature Writer
}

//  pgpSplitWriter implements the PGPSplitWriter interface

func (mw *pgpSplitWriter) Keys() Writer {
	return mw.keyPackets
}

func (mw *pgpSplitWriter) Write(b []byte) (int, error) {
	return mw.ciphertext.Write(b)
}

func (mw *pgpSplitWriter) Signature() Writer {
	return mw.detachedSignature
}

func NewPGPSplitWriter(keyPackets Writer, encPackets Writer, encSigPacket Writer) PGPSplitWriter {
	return &pgpSplitWriter{
		keyPackets:        keyPackets,
		ciphertext:        encPackets,
		detachedSignature: encSigPacket,
	}
}

func NewPGPSplitWriterKeyAndData(keyPackets Writer, encPackets Writer) PGPSplitWriter {
	return NewPGPSplitWriter(keyPackets, encPackets, nil)
}

func NewPGPSplitWriterDetachedSignature(encMessage Writer, encSigMessage Writer) PGPSplitWriter {
	return NewPGPSplitWriter(nil, encMessage, encSigMessage)
}

func NewPGPSplitWriterFromWriter(writer Writer) PGPSplitWriter {
	return NewPGPSplitWriter(writer, writer, nil)
}

type signAndEncryptWriteCloser struct {
	signWriter    WriteCloser
	encryptWriter WriteCloser
}

func (w *signAndEncryptWriteCloser) Write(b []byte) (int, error) {
	return w.signWriter.Write(b)
}

func (w *signAndEncryptWriteCloser) Close() error {
	if err := w.signWriter.Close(); err != nil {
		return err
	}
	return w.encryptWriter.Close()
}

func (eh *encryptionHandle) prepareEncryptAndSign(
	plainMessageMetadata *LiteralMetadata,
) (hints *openpgp.FileHints, config *packet.Config, signEntity *openpgp.Entity, err error) {
	hints = &openpgp.FileHints{
		FileName: plainMessageMetadata.GetFilename(),
		IsUTF8:   plainMessageMetadata.GetIsUtf8(),
		ModTime:  time.Unix(plainMessageMetadata.GetTime(), 0),
	}

	config = eh.profile.EncryptionConfig()
	config.Time = eh.clock

	if eh.Compression {
		compressionConfig := eh.profile.CompressionConfig()
		config.DefaultCompressionAlgo = compressionConfig.DefaultCompressionAlgo
		config.CompressionConfig = compressionConfig.CompressionConfig
	}

	if eh.SigningContext != nil {
		config.SignatureNotations = append(config.SignatureNotations, eh.SigningContext.getNotation())
	}

	if eh.SignKeyRing != nil && len(eh.SignKeyRing.entities) > 0 {
		signEntity, err = eh.SignKeyRing.getSigningEntity()
		if err != nil {
			return
		}
	}
	return
}

func (eh *encryptionHandle) encryptStream(
	keyPacketWriter Writer,
	dataPacketWriter Writer,
	plainMessageMetadata *LiteralMetadata,
) (plainMessageWriter WriteCloser, err error) {
	var sessionKeyBytes []byte
	if eh.SessionKey != nil {
		sessionKeyBytes = eh.SessionKey.Key
	}
	hints, config, signEntity, err := eh.prepareEncryptAndSign(plainMessageMetadata)
	if err != nil {
		return
	}
	var signers []*openpgp.Entity
	if signEntity != nil {
		signers = []*openpgp.Entity{signEntity}
	}
	plainMessageWriter, err = openpgp.EncryptWithParams(
		dataPacketWriter,
		eh.Recipients.getEntities(),
		eh.HiddenRecipients.getEntities(),
		&openpgp.EncryptParams{
			KeyWriter:  keyPacketWriter,
			Signers:    signers,
			Hints:      hints,
			SessionKey: sessionKeyBytes,
			Config:     config,
			TextSig:    eh.IsUTF8,
		},
	)
	if err != nil {
		return nil, errors.Wrap(err, "gopenpgp: error in encrypting asymmetrically")
	}
	return plainMessageWriter, nil
}

func (eh *encryptionHandle) encryptStreamWithPassword(
	keyPacketWriter Writer,
	dataPacketWriter Writer,
	plainMessageMetadata *LiteralMetadata,
) (plainMessageWriter io.WriteCloser, err error) {
	var sessionKeyBytes []byte
	if eh.SessionKey != nil {
		sessionKeyBytes = eh.SessionKey.Key
	}
	hints, config, signEntity, err := eh.prepareEncryptAndSign(plainMessageMetadata)
	if err != nil {
		return
	}

	var signers []*openpgp.Entity
	if signEntity != nil {
		signers = []*openpgp.Entity{signEntity}
	}
	plainMessageWriter, err = openpgp.SymmetricallyEncryptWithParams(
		eh.Password,
		dataPacketWriter,
		&openpgp.EncryptParams{
			KeyWriter:  keyPacketWriter,
			Signers:    signers,
			Hints:      hints,
			SessionKey: sessionKeyBytes,
			Config:     config,
			TextSig:    eh.IsUTF8,
		},
	)
	if err != nil {
		return nil, errors.Wrap(err, "gopenpgp: error in encrypting asymmetrically")
	}
	return plainMessageWriter, nil
}

func (eh *encryptionHandle) encryptStreamWithSessionKey(
	dataPacketWriter Writer,
	plainMessageMetadata *LiteralMetadata,
) (plainMessageWriter WriteCloser, err error) {
	encryptWriter, signWriter, err := eh.encryptStreamWithSessionKeyHelper(
		plainMessageMetadata,
		dataPacketWriter,
	)

	if err != nil {
		return nil, err
	}
	if signWriter != nil {
		plainMessageWriter = &signAndEncryptWriteCloser{signWriter, encryptWriter}
	} else {
		plainMessageWriter = encryptWriter
	}
	return plainMessageWriter, err
}

func (eh *encryptionHandle) encryptStreamWithSessionKeyHelper(
	plainMessageMetadata *LiteralMetadata,
	dataPacketWriter io.Writer,
) (encryptWriter, signWriter io.WriteCloser, err error) {
	hints, config, signEntity, err := eh.prepareEncryptAndSign(plainMessageMetadata)
	if err != nil {
		return
	}

	if !eh.SessionKey.v6 {
		config.DefaultCipher, err = eh.SessionKey.GetCipherFunc()
		if err != nil {
			return nil, nil, errors.Wrap(err, "gopenpgp: unable to encrypt with session key")
		}
	}

	encryptWriter, err = packet.SerializeSymmetricallyEncrypted(
		dataPacketWriter,
		config.Cipher(),
		config.AEAD() != nil,
		packet.CipherSuite{Cipher: config.Cipher(), Mode: config.AEAD().Mode()},
		eh.SessionKey.Key,
		config,
	)

	if err != nil {
		return nil, nil, errors.Wrap(err, "gopenpgp: unable to encrypt")
	}

	if algo := config.Compression(); algo != packet.CompressionNone {
		encryptWriter, err = packet.SerializeCompressed(encryptWriter, algo, config.CompressionConfig)
		if err != nil {
			return nil, nil, errors.Wrap(err, "gopenpgp: error in compression")
		}
	}

	if signEntity != nil {
		signWriter, err = openpgp.SignWithParams(encryptWriter, []*openpgp.Entity{signEntity}, &openpgp.SignParams{
			Hints:   hints,
			TextSig: eh.IsUTF8,
			Config:  config,
		})
		if err != nil {
			return nil, nil, errors.Wrap(err, "gopenpgp: unable to sign")
		}
	} else {
		encryptWriter, err = packet.SerializeLiteral(
			encryptWriter,
			plainMessageMetadata.GetIsUtf8(),
			plainMessageMetadata.GetFilename(),
			uint32(plainMessageMetadata.GetTime()),
		)
		if err != nil {
			return nil, nil, errors.Wrap(err, "gopenpgp: unable to serialize")
		}
	}
	return encryptWriter, signWriter, nil
}

type encryptSignDetachedWriter struct {
	ptToCiphertextWriter  WriteCloser
	sigToCiphertextWriter WriteCloser
	ptToEncSigWriter      WriteCloser
	ptWriter              Writer
}

func (w *encryptSignDetachedWriter) Write(b []byte) (int, error) {
	return w.ptWriter.Write(b)
}

func (w *encryptSignDetachedWriter) Close() error {
	if err := w.ptToCiphertextWriter.Close(); err != nil {
		return err
	}
	if err := w.ptToEncSigWriter.Close(); err != nil {
		return err
	}
	return w.sigToCiphertextWriter.Close()
}

// encryptSignDetachedStreamWithSessionKey wraps writers to encrypt a message
// with a session key and produces a detached signature for the plaintext.
func (eh *encryptionHandle) encryptSignDetachedStreamWithSessionKey(
	plainMessageMetadata *LiteralMetadata,
	encryptedSignatureWriter io.Writer,
	encryptedDataWriter io.Writer,
) (io.WriteCloser, error) {
	signKeyRing := eh.SignKeyRing
	eh.SignKeyRing = nil
	defer func() {
		eh.SignKeyRing = signKeyRing
	}()
	// Create a writer to encrypt the message.
	ptToCiphertextWriter, err := eh.encryptStreamWithSessionKey(encryptedDataWriter, plainMessageMetadata)
	if err != nil {
		return nil, err
	}
	// Create a writer to encrypt the signature.
	sigToCiphertextWriter, err := eh.encryptStreamWithSessionKey(encryptedSignatureWriter, nil)
	if err != nil {
		return nil, err
	}
	// Create a writer to sign the message.
	ptToEncSigWriter, err := signMessageDetachedWriter(
		signKeyRing,
		sigToCiphertextWriter,
		eh.IsUTF8,
		eh.SigningContext,
		eh.clock,
		eh.profile.EncryptionConfig(),
	)
	if err != nil {
		return nil, err
	}

	// Return a wrapped plaintext writer that writes encrypted data and the encrypted signature.
	return &encryptSignDetachedWriter{
		ptToCiphertextWriter:  ptToCiphertextWriter,
		sigToCiphertextWriter: sigToCiphertextWriter,
		ptToEncSigWriter:      ptToEncSigWriter,
		ptWriter:              io.MultiWriter(ptToCiphertextWriter, ptToEncSigWriter),
	}, nil
}

func (eh *encryptionHandle) encryptSignDetachedStreamToRecipients(
	plainMessageMetadata *LiteralMetadata,
	encryptedSignatureWriter io.Writer,
	encryptedDataWriter io.Writer,
	keyPacketWriter io.Writer,
) (plaintextWriter io.WriteCloser, err error) {
	configInput := eh.profile.EncryptionConfig()
	configInput.Time = NewConstantClock(eh.clock().Unix())
	// Generate a session key for encryption.
	if eh.SessionKey == nil {
		eh.SessionKey, err = generateSessionKey(configInput)
		if err != nil {
			return nil, err
		}
		defer func() {
			eh.SessionKey.Clear()
			eh.SessionKey = nil
		}()
	}
	if keyPacketWriter == nil {
		// If no separate keyPacketWriter is given, write the key packets
		// as prefix to the encrypted data and encrypted signature
		keyPacketWriter = io.MultiWriter(encryptedDataWriter, encryptedSignatureWriter)
	}

	if eh.Recipients != nil || eh.HiddenRecipients != nil {
		// Encrypt the session key to the different recipients.
		err = encryptSessionKeyToWriter(
			eh.Recipients,
			eh.HiddenRecipients,
			eh.SessionKey,
			keyPacketWriter,
			configInput,
		)
	} else if eh.Password != nil {
		// If not recipients present use the provided password
		err = encryptSessionKeyWithPasswordToWriter(
			eh.Password,
			eh.SessionKey,
			keyPacketWriter,
			configInput,
		)
	} else {
		err = errors.New("openpgp: no key material to encrypt")
	}

	if err != nil {
		return nil, err
	}
	// Use the session key to encrypt message + signature of the message.
	plaintextWriter, err = eh.encryptSignDetachedStreamWithSessionKey(
		plainMessageMetadata,
		encryptedSignatureWriter,
		encryptedDataWriter,
	)
	if err != nil {
		return nil, err
	}
	return plaintextWriter, err
}