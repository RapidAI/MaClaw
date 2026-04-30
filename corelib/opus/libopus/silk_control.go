package libopus

import "github.com/RapidAI/CodeClaw/corelib/opus/silk"

const FLAG_DECODE_NORMAL = 0
const FLAG_PACKET_LOST = 1
const FLAG_DECODE_LBRR = 2

type silk_EncControlStruct = silk.EncControlStruct
type silk_DecControlStruct = silk.DecControlStruct
