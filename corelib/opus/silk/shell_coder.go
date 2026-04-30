package silk

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/entcode"
)

func combine_pulses(out []int, in []int, len_ int) {
	var k int
	for k = 0; k < len_; k++ {
		out[k] = in[k*2] + in[k*2+1]
	}
}
func encode_split(psRangeEnc *entcode.Encoder, p_child1 int, p int, shell_table []byte) {
	if p > 0 {
		psRangeEnc.EncIcdf(p_child1, shell_table[silk_shell_code_table_offsets[p]:], 8)
	}
}
func decode_split(p_child1 *int16, p_child2 *int16, psRangeDec *entcode.Decoder, p int, shell_table []byte) {
	if p > 0 {
		*p_child1 = int16(psRangeDec.DecIcdf(shell_table[silk_shell_code_table_offsets[p]:], 8))
		*p_child2 = int16(p - int(*p_child1))
	} else {
		*p_child1 = 0
		*p_child2 = 0
	}
}
func ShellEncoder(psRangeEnc *entcode.Encoder, pulses0 []int) {
	var (
		pulses1 [8]int
		pulses2 [4]int
		pulses3 [2]int
		pulses4 [1]int
	)
	combine_pulses(pulses1[:], pulses0, 8)
	combine_pulses(pulses2[:], pulses1[:], 4)
	combine_pulses(pulses3[:], pulses2[:], 2)
	combine_pulses(pulses4[:], pulses3[:], 1)
	encode_split(psRangeEnc, pulses3[0], pulses4[0], silk_shell_code_table3[:])
	encode_split(psRangeEnc, pulses2[0], pulses3[0], silk_shell_code_table2[:])
	encode_split(psRangeEnc, pulses1[0], pulses2[0], silk_shell_code_table1[:])
	encode_split(psRangeEnc, pulses0[0], pulses1[0], silk_shell_code_table0[:])
	encode_split(psRangeEnc, pulses0[2], pulses1[1], silk_shell_code_table0[:])
	encode_split(psRangeEnc, pulses1[2], pulses2[1], silk_shell_code_table1[:])
	encode_split(psRangeEnc, pulses0[4], pulses1[2], silk_shell_code_table0[:])
	encode_split(psRangeEnc, pulses0[6], pulses1[3], silk_shell_code_table0[:])
	encode_split(psRangeEnc, pulses2[2], pulses3[1], silk_shell_code_table2[:])
	encode_split(psRangeEnc, pulses1[4], pulses2[2], silk_shell_code_table1[:])
	encode_split(psRangeEnc, pulses0[8], pulses1[4], silk_shell_code_table0[:])
	encode_split(psRangeEnc, pulses0[10], pulses1[5], silk_shell_code_table0[:])
	encode_split(psRangeEnc, pulses1[6], pulses2[3], silk_shell_code_table1[:])
	encode_split(psRangeEnc, pulses0[12], pulses1[6], silk_shell_code_table0[:])
	encode_split(psRangeEnc, pulses0[14], pulses1[7], silk_shell_code_table0[:])
}
func shellDecoder(pulses0 []int16, psRangeDec *entcode.Decoder, pulses4 int) {
	var (
		pulses3 [2]int16
		pulses2 [4]int16
		pulses1 [8]int16
	)
	decode_split(&pulses3[0], &pulses3[1], psRangeDec, pulses4, silk_shell_code_table3[:])
	decode_split(&pulses2[0], &pulses2[1], psRangeDec, int(pulses3[0]), silk_shell_code_table2[:])
	decode_split(&pulses1[0], &pulses1[1], psRangeDec, int(pulses2[0]), silk_shell_code_table1[:])
	decode_split(&pulses0[0], &pulses0[1], psRangeDec, int(pulses1[0]), silk_shell_code_table0[:])
	decode_split(&pulses0[2], &pulses0[3], psRangeDec, int(pulses1[1]), silk_shell_code_table0[:])
	decode_split(&pulses1[2], &pulses1[3], psRangeDec, int(pulses2[1]), silk_shell_code_table1[:])
	decode_split(&pulses0[4], &pulses0[5], psRangeDec, int(pulses1[2]), silk_shell_code_table0[:])
	decode_split(&pulses0[6], &pulses0[7], psRangeDec, int(pulses1[3]), silk_shell_code_table0[:])
	decode_split(&pulses2[2], &pulses2[3], psRangeDec, int(pulses3[1]), silk_shell_code_table2[:])
	decode_split(&pulses1[4], &pulses1[5], psRangeDec, int(pulses2[2]), silk_shell_code_table1[:])
	decode_split(&pulses0[8], &pulses0[9], psRangeDec, int(pulses1[4]), silk_shell_code_table0[:])
	decode_split(&pulses0[10], &pulses0[11], psRangeDec, int(pulses1[5]), silk_shell_code_table0[:])
	decode_split(&pulses1[6], &pulses1[7], psRangeDec, int(pulses2[3]), silk_shell_code_table1[:])
	decode_split(&pulses0[12], &pulses0[13], psRangeDec, int(pulses1[6]), silk_shell_code_table0[:])
	decode_split(&pulses0[14], &pulses0[15], psRangeDec, int(pulses1[7]), silk_shell_code_table0[:])
}
