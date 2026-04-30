package libopus

var silk_stereo_pred_quant_Q13 [16]int16 = [16]int16{-13732, -10050, -8266, -7526, -6500, -5000, -2950, -820, 820, 2950, 5000, 6500, 7526, 8266, 10050, 13732}
var silk_stereo_pred_joint_iCDF [25]uint8 = [25]uint8{249, 247, 246, 245, 244, 234, 210, 202, 201, 200, 197, 174, 82, 59, 56, 55, 54, 46, 22, 12, 11, 10, 9, 7, 0}
var silk_stereo_only_code_mid_iCDF [2]uint8 = [2]uint8{64, 0}
var silk_LBRR_flags_2_iCDF [3]uint8 = [3]uint8{203, 150, 0}
var silk_LBRR_flags_3_iCDF [7]uint8 = [7]uint8{215, 195, 166, 125, 110, 82, 0}
var silk_LBRR_flags_iCDF_ptr [2]*uint8 = [2]*uint8{&silk_LBRR_flags_2_iCDF[0], &silk_LBRR_flags_3_iCDF[0]}
var silk_lsb_iCDF [2]uint8 = [2]uint8{120, 0}
var silk_LTPscale_iCDF [3]uint8 = [3]uint8{128, 64, 0}
var silk_type_offset_VAD_iCDF [4]uint8 = [4]uint8{232, 158, 10, 0}
var silk_type_offset_no_VAD_iCDF [2]uint8 = [2]uint8{230, 0}
var silk_NLSF_interpolation_factor_iCDF [5]uint8 = [5]uint8{243, 221, 192, 181, 0}
var silk_Quantization_Offsets_Q10 [2][2]int16 = [2][2]int16{{OFFSET_UVL_Q10, OFFSET_UVH_Q10}, {OFFSET_VL_Q10, OFFSET_VH_Q10}}
var silk_LTPScales_table_Q14 [3]int16 = [3]int16{15565, 12288, 8192}
var silk_uniform3_iCDF [3]uint8 = [3]uint8{171, 85, 0}
var silk_uniform4_iCDF [4]uint8 = [4]uint8{192, 128, 64, 0}
var silk_uniform5_iCDF [5]uint8 = [5]uint8{205, 154, 102, 51, 0}
var silk_uniform6_iCDF [6]uint8 = [6]uint8{213, 171, 128, 85, 43, 0}
var silk_uniform8_iCDF [8]uint8 = [8]uint8{224, 192, 160, 128, 96, 64, 32, 0}
var silk_NLSF_EXT_iCDF [7]uint8 = [7]uint8{100, 40, 16, 7, 3, 1, 0}
var silk_Transition_LP_B_Q28 [5][3]int32 = [5][3]int32{{250767114, 501534038, 250767114}, {209867381, 419732057, 209867381}, {170987846, 341967853, 170987846}, {131531482, 263046905, 131531482}, {89306658, 178584282, 89306658}}
var silk_Transition_LP_A_Q28 [5][2]int32 = [5][2]int32{{506393414, 239854379}, {411067935, 169683996}, {306733530, 116694253}, {185807084, 77959395}, {35497197, 57401098}}
