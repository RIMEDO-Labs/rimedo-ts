package monitoring

type Type int32

const (
	// ASN1 :
	ASN1 Type = iota

	// PROTO :
	PROTO
)

// String :
func (t Type) String() string {
	return [...]string{"ASN.1", "PROTO"}[t]
}

type Indication struct {
	// EncodingType payload encoding type
	EncodingType Type

	// Payload is the indication payload
	Payload Payload
}

// Payload is an E2 indication payload
type Payload struct {
	Header  []byte
	Message []byte
}
