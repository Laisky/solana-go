package solana

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// raisedV1Limits is the SIMD-0596 (Draft) address limit.
var raisedV1Limits = V1Limits{
	MaxSignatures:   MaxSignaturesV1,
	MaxAddresses:    96,
	MaxInstructions: MaxInstructionsV1,
}

// v1MessageWithAddresses returns a valid v1 message with n account addresses:
// one writable signer (the fee payer), a program at index 1, and n-2 readonly
// unsigned accounts.
func v1MessageWithAddresses(t *testing.T, n int) Message {
	t.Helper()
	msg := createTestV1Message(t)
	msg.AccountKeys = uniqueKeys(n)
	msg.Header = MessageHeader{NumRequiredSignatures: 1, NumReadonlyUnsignedAccounts: 1}
	msg.Instructions[0].ProgramIDIndex = 1
	msg.Instructions[0].Accounts = []uint16{0, 2}
	return msg
}

func TestDefaultV1Limits(t *testing.T) {
	assert.Equal(t, V1Limits{MaxSignatures: 12, MaxAddresses: 64, MaxInstructions: 64}, DefaultV1Limits())
}

// Sanitize and SanitizeWithV1Limits(DefaultV1Limits()) agree on both sides of
// every default limit.
func TestV1SanitizeWithV1Limits_DefaultMatchesSanitize(t *testing.T) {
	cases := map[string]func(t *testing.T) Message{
		"64 addresses": func(t *testing.T) Message { return v1MessageWithAddresses(t, 64) },
		"65 addresses": func(t *testing.T) Message { return v1MessageWithAddresses(t, 65) },
		"12 signatures": func(t *testing.T) Message {
			msg := createTestV1Message(t)
			msg.Header.NumRequiredSignatures = 12
			msg.AccountKeys = uniqueKeys(14)
			msg.Instructions[0] = CompiledInstruction{ProgramIDIndex: 13, Accounts: []uint16{0}}
			return msg
		},
		"13 signatures": func(t *testing.T) Message {
			msg := createTestV1Message(t)
			msg.Header.NumRequiredSignatures = 13
			msg.AccountKeys = uniqueKeys(15)
			msg.Instructions[0] = CompiledInstruction{ProgramIDIndex: 14, Accounts: []uint16{0}}
			return msg
		},
		"64 instructions": func(t *testing.T) Message { return v1MessageWithInstructions(t, 64) },
		"65 instructions": func(t *testing.T) Message { return v1MessageWithInstructions(t, 65) },
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			msg := build(t)
			want := msg.Sanitize()
			got := msg.SanitizeWithV1Limits(DefaultV1Limits())
			if want == nil {
				require.NoError(t, got)
				return
			}
			require.Error(t, got)
			assert.Equal(t, want.Error(), got.Error())
			assert.True(t, IsSanitizeError(got))
		})
	}
}

func v1MessageWithInstructions(t *testing.T, n int) Message {
	t.Helper()
	msg := createTestV1Message(t)
	msg.Instructions = make([]CompiledInstruction, n)
	for i := range msg.Instructions {
		msg.Instructions[i] = CompiledInstruction{ProgramIDIndex: 1, Accounts: []uint16{0, 2}}
	}
	return msg
}

func TestV1SanitizeWithV1Limits_RaisedAddressLimit(t *testing.T) {
	msg := v1MessageWithAddresses(t, 96)
	require.NoError(t, msg.SanitizeWithV1Limits(raisedV1Limits))

	err := msg.Sanitize()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many addresses: 96 > max 64")

	msg = v1MessageWithAddresses(t, 97)
	err = msg.SanitizeWithV1Limits(raisedV1Limits)
	require.Error(t, err)
	assert.True(t, IsSanitizeError(err))
	assert.Contains(t, err.Error(), "too many addresses: 97 > max 96")
}

func TestV1SanitizeWithV1Limits_SignatureAndInstructionLimits(t *testing.T) {
	msg := createTestV1Message(t)
	msg.Header.NumRequiredSignatures = 2
	msg.AccountKeys = uniqueKeys(4)
	msg.Instructions[0] = CompiledInstruction{ProgramIDIndex: 3, Accounts: []uint16{0}}
	require.NoError(t, msg.SanitizeWithV1Limits(DefaultV1Limits()))

	limits := DefaultV1Limits()
	limits.MaxSignatures = 1
	err := msg.SanitizeWithV1Limits(limits)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many signatures: required_signatures(2) > max 1")

	msg = v1MessageWithInstructions(t, 65)
	limits = DefaultV1Limits()
	limits.MaxInstructions = 65
	require.NoError(t, msg.SanitizeWithV1Limits(limits))
	limits.MaxInstructions = 1
	err = msg.SanitizeWithV1Limits(limits)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many instructions: 65 > max 1")
}

// The zero value fails closed instead of meaning "no limit".
func TestV1SanitizeWithV1Limits_ZeroLimitsRejectEveryV1Message(t *testing.T) {
	msg := createTestV1Message(t)
	require.NoError(t, msg.Sanitize())
	err := msg.SanitizeWithV1Limits(V1Limits{})
	require.Error(t, err)
	assert.True(t, IsSanitizeError(err))
}

// Raising the policy limits never relaxes the other v1 checks.
func TestV1SanitizeWithV1Limits_OtherChecksUnchanged(t *testing.T) {
	permissive := V1Limits{MaxSignatures: 255, MaxAddresses: 255, MaxInstructions: 255}

	msg := createTestV1Message(t)
	msg.AccountKeys[1] = msg.AccountKeys[0]
	err := msg.SanitizeWithV1Limits(permissive)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")

	msg = createTestV1Message(t)
	msg.TransactionConfig = msg.TransactionConfig.WithHeapSize(MinHeapSizeV1 + 1)
	err = msg.SanitizeWithV1Limits(permissive)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "heap size")

	msg = createTestV1Message(t)
	msg.AddAddressTableLookup(MessageAddressTableLookup{AccountKey: newUniqueKey(), WritableIndexes: []uint8{0}})
	err = msg.SanitizeWithV1Limits(permissive)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "address table lookups")

	msg = createTestV1Message(t)
	msg.Header.NumReadonlySignedAccounts = 1
	err = msg.SanitizeWithV1Limits(permissive)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no writable signer")

	msg = createTestV1Message(t)
	msg.Instructions[0].Accounts = []uint16{0, 99}
	err = msg.SanitizeWithV1Limits(permissive)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account index 99 out of bounds")
}

func TestSanitizeWithV1Limits_LegacyAndV0IgnoreLimits(t *testing.T) {
	legacy := Message{
		Header:      MessageHeader{NumRequiredSignatures: 1, NumReadonlyUnsignedAccounts: 1},
		AccountKeys: PublicKeySlice(uniqueKeys(70)),
		Instructions: []CompiledInstruction{
			{ProgramIDIndex: 1, Accounts: []uint16{0}},
		},
	}
	legacy.version = MessageVersionLegacy
	require.NoError(t, legacy.Sanitize())
	require.NoError(t, legacy.SanitizeWithV1Limits(V1Limits{}))

	v0 := Message{
		Header:      MessageHeader{NumRequiredSignatures: 1},
		AccountKeys: PublicKeySlice{newUniqueKey()},
	}
	v0.version = MessageVersionV0
	require.NoError(t, v0.Sanitize())
	require.NoError(t, v0.SanitizeWithV1Limits(V1Limits{}))

	// Invalid legacy messages stay invalid whatever the limits.
	legacy.Header.NumReadonlySignedAccounts = 1
	require.Error(t, legacy.SanitizeWithV1Limits(V1Limits{MaxSignatures: 255, MaxAddresses: 255, MaxInstructions: 255}))
}

func TestTransactionSanitizeWithV1Limits(t *testing.T) {
	tx := &Transaction{
		Message:    v1MessageWithAddresses(t, 96),
		Signatures: []Signature{{}},
	}
	err := tx.Sanitize()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many addresses")
	require.NoError(t, tx.SanitizeWithV1Limits(raisedV1Limits))

	// The signature-count checks still run before the message checks.
	tx.Signatures = nil
	err = tx.SanitizeWithV1Limits(raisedV1Limits)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not enough signatures")

	tx.Signatures = []Signature{{}, {}}
	err = tx.SanitizeWithV1Limits(raisedV1Limits)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too many signatures: required 1, got 2")
}

// The wire format is not limited to 64 addresses, so a 96-address v1 message
// round-trips; only the policy limit decides whether it is valid.
func TestV1_96AddressMessageRoundTrip(t *testing.T) {
	msg := v1MessageWithAddresses(t, 96)
	raw := mustMarshalV1(t, &msg)
	back := mustUnmarshalV1(t, raw)

	assert.Equal(t, MessageVersionV1, back.GetVersion())
	assert.Equal(t, msg.AccountKeys, back.AccountKeys)
	assert.Equal(t, msg.Header, back.Header)
	assert.Equal(t, msg.Instructions, back.Instructions)
	assert.Equal(t, raw, mustMarshalV1(t, &back))

	require.Error(t, back.Sanitize())
	require.NoError(t, back.SanitizeWithV1Limits(raisedV1Limits))
}

func TestNewTransaction_V1Limits(t *testing.T) {
	payer := newUniqueKey()
	prog := newUniqueKey()
	bh := uniqueHash()
	metas := AccountMetaSlice{Meta(payer).WRITE().SIGNER()}
	for range 94 {
		metas = append(metas, Meta(newUniqueKey()))
	}
	ix := NewInstruction(prog, metas, nil)

	_, err := NewTransaction([]Instruction{ix}, bh, TransactionPayer(payer), TransactionMessageVersion(MessageVersionV1))
	require.Error(t, err)
	assert.True(t, IsSanitizeError(err))

	tx, err := NewTransaction([]Instruction{ix}, bh,
		TransactionPayer(payer),
		TransactionMessageVersion(MessageVersionV1),
		TransactionV1Limits(raisedV1Limits),
	)
	require.NoError(t, err)
	assert.Equal(t, MessageVersionV1, tx.Message.GetVersion())
	assert.Len(t, tx.Message.AccountKeys, 96)
	require.NoError(t, tx.Message.SanitizeWithV1Limits(raisedV1Limits))

	// The option does not select v1 by itself.
	tx, err = NewTransaction([]Instruction{ix}, bh, TransactionPayer(payer), TransactionV1Limits(V1Limits{}))
	require.NoError(t, err)
	assert.Equal(t, MessageVersionLegacy, tx.Message.GetVersion())
}
