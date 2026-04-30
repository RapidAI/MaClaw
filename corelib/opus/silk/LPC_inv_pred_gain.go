package silk

import "math"

const QA = 24

func LPC_inverse_pred_gain_QA_c(A_QA [24]int32, order int) int32 {
	var (
		k            int
		n            int
		mult2Q       int
		invGain_Q30  int32
		rc_Q31       int32
		rc_mult1_Q30 int32
		rc_mult2     int32
		tmp1         int32
		tmp2         int32
	)
	invGain_Q30 = int32(math.Floor(1*(1<<30) + 0.5))
	for k = order - 1; k > 0; k-- {
		tf := float64(int(1<<QA))*0.99975 + 0.5
		if int(A_QA[k]) > int(int32(tf)) || int(A_QA[k]) < int(-(int32(tf))) {
			return 0
		}
		rc_Q31 = -(int32(int(uint32(A_QA[k])) << (int(31 - QA))))
		rc_mult1_Q30 = int32(int(int32(math.Floor(1*(1<<30)+0.5))) - int(int32((int64(rc_Q31)*int64(rc_Q31))>>32)))
		invGain_Q30 = int32(int(uint32(int32((int64(invGain_Q30)*int64(rc_mult1_Q30))>>32))) << 2)
		if int(invGain_Q30) < int(int32(math.Floor((1.0/MAX_PREDICTION_POWER_GAIN)*(1<<30)+0.5))) {
			return 0
		}
		mult2Q = 32 - int(silk_CLZ32(int32(func() int {
			if int(rc_mult1_Q30) > 0 {
				return int(rc_mult1_Q30)
			}
			return int(-rc_mult1_Q30)
		}())))
		rc_mult2 = silk_INVERSE32_varQ(rc_mult1_Q30, mult2Q+30)
		for n = 0; n < (k+1)>>1; n++ {
			var tmp64 int64
			tmp1 = A_QA[n]
			tmp2 = A_QA[k-n-1]
			if mult2Q == 1 {
				tmp64 = ((int64(func() int {
					if ((int(uint32(tmp1)) - int(uint32(int32(func() int64 {
						if 31 == 1 {
							return ((int64(tmp2) * int64(rc_Q31)) >> 1) + ((int64(tmp2) * int64(rc_Q31)) & 1)
						}
						return (((int64(tmp2) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
					}())))) & 0x80000000) == 0 {
						if (int(tmp1) & (int(int32(func() int64 {
							if 31 == 1 {
								return ((int64(tmp2) * int64(rc_Q31)) >> 1) + ((int64(tmp2) * int64(rc_Q31)) & 1)
							}
							return (((int64(tmp2) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
						}())) ^ 0x80000000) & 0x80000000) != 0 {
							return math.MinInt32
						}
						return int(tmp1) - int(int32(func() int64 {
							if 31 == 1 {
								return ((int64(tmp2) * int64(rc_Q31)) >> 1) + ((int64(tmp2) * int64(rc_Q31)) & 1)
							}
							return (((int64(tmp2) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
						}()))
					}
					if ((int(tmp1) ^ 0x80000000) & int(int32(func() int64 {
						if 31 == 1 {
							return ((int64(tmp2) * int64(rc_Q31)) >> 1) + ((int64(tmp2) * int64(rc_Q31)) & 1)
						}
						return (((int64(tmp2) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
					}())) & 0x80000000) != 0 {
						return math.MaxInt32
					}
					return int(tmp1) - int(int32(func() int64 {
						if 31 == 1 {
							return ((int64(tmp2) * int64(rc_Q31)) >> 1) + ((int64(tmp2) * int64(rc_Q31)) & 1)
						}
						return (((int64(tmp2) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
					}()))
				}()) * int64(rc_mult2)) >> 1) + ((int64(func() int {
					if ((int(uint32(tmp1)) - int(uint32(int32(func() int64 {
						if 31 == 1 {
							return ((int64(tmp2) * int64(rc_Q31)) >> 1) + ((int64(tmp2) * int64(rc_Q31)) & 1)
						}
						return (((int64(tmp2) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
					}())))) & 0x80000000) == 0 {
						if (int(tmp1) & (int(int32(func() int64 {
							if 31 == 1 {
								return ((int64(tmp2) * int64(rc_Q31)) >> 1) + ((int64(tmp2) * int64(rc_Q31)) & 1)
							}
							return (((int64(tmp2) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
						}())) ^ 0x80000000) & 0x80000000) != 0 {
							return math.MinInt32
						}
						return int(tmp1) - int(int32(func() int64 {
							if 31 == 1 {
								return ((int64(tmp2) * int64(rc_Q31)) >> 1) + ((int64(tmp2) * int64(rc_Q31)) & 1)
							}
							return (((int64(tmp2) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
						}()))
					}
					if ((int(tmp1) ^ 0x80000000) & int(int32(func() int64 {
						if 31 == 1 {
							return ((int64(tmp2) * int64(rc_Q31)) >> 1) + ((int64(tmp2) * int64(rc_Q31)) & 1)
						}
						return (((int64(tmp2) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
					}())) & 0x80000000) != 0 {
						return math.MaxInt32
					}
					return int(tmp1) - int(int32(func() int64 {
						if 31 == 1 {
							return ((int64(tmp2) * int64(rc_Q31)) >> 1) + ((int64(tmp2) * int64(rc_Q31)) & 1)
						}
						return (((int64(tmp2) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
					}()))
				}()) * int64(rc_mult2)) & 1)
			} else {
				tmp64 = (((int64(func() int {
					if ((int(uint32(tmp1)) - int(uint32(int32(func() int64 {
						if 31 == 1 {
							return ((int64(tmp2) * int64(rc_Q31)) >> 1) + ((int64(tmp2) * int64(rc_Q31)) & 1)
						}
						return (((int64(tmp2) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
					}())))) & 0x80000000) == 0 {
						if (int(tmp1) & (int(int32(func() int64 {
							if 31 == 1 {
								return ((int64(tmp2) * int64(rc_Q31)) >> 1) + ((int64(tmp2) * int64(rc_Q31)) & 1)
							}
							return (((int64(tmp2) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
						}())) ^ 0x80000000) & 0x80000000) != 0 {
							return math.MinInt32
						}
						return int(tmp1) - int(int32(func() int64 {
							if 31 == 1 {
								return ((int64(tmp2) * int64(rc_Q31)) >> 1) + ((int64(tmp2) * int64(rc_Q31)) & 1)
							}
							return (((int64(tmp2) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
						}()))
					}
					if ((int(tmp1) ^ 0x80000000) & int(int32(func() int64 {
						if 31 == 1 {
							return ((int64(tmp2) * int64(rc_Q31)) >> 1) + ((int64(tmp2) * int64(rc_Q31)) & 1)
						}
						return (((int64(tmp2) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
					}())) & 0x80000000) != 0 {
						return math.MaxInt32
					}
					return int(tmp1) - int(int32(func() int64 {
						if 31 == 1 {
							return ((int64(tmp2) * int64(rc_Q31)) >> 1) + ((int64(tmp2) * int64(rc_Q31)) & 1)
						}
						return (((int64(tmp2) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
					}()))
				}()) * int64(rc_mult2)) >> int64(mult2Q-1)) + 1) >> 1
			}
			if tmp64 > math.MaxInt32 || tmp64 < int64(math.MinInt32) {
				return 0
			}
			A_QA[n] = int32(tmp64)
			if mult2Q == 1 {
				tmp64 = ((int64(func() int {
					if ((int(uint32(tmp2)) - int(uint32(int32(func() int64 {
						if 31 == 1 {
							return ((int64(tmp1) * int64(rc_Q31)) >> 1) + ((int64(tmp1) * int64(rc_Q31)) & 1)
						}
						return (((int64(tmp1) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
					}())))) & 0x80000000) == 0 {
						if (int(tmp2) & (int(int32(func() int64 {
							if 31 == 1 {
								return ((int64(tmp1) * int64(rc_Q31)) >> 1) + ((int64(tmp1) * int64(rc_Q31)) & 1)
							}
							return (((int64(tmp1) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
						}())) ^ 0x80000000) & 0x80000000) != 0 {
							return math.MinInt32
						}
						return int(tmp2) - int(int32(func() int64 {
							if 31 == 1 {
								return ((int64(tmp1) * int64(rc_Q31)) >> 1) + ((int64(tmp1) * int64(rc_Q31)) & 1)
							}
							return (((int64(tmp1) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
						}()))
					}
					if ((int(tmp2) ^ 0x80000000) & int(int32(func() int64 {
						if 31 == 1 {
							return ((int64(tmp1) * int64(rc_Q31)) >> 1) + ((int64(tmp1) * int64(rc_Q31)) & 1)
						}
						return (((int64(tmp1) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
					}())) & 0x80000000) != 0 {
						return math.MaxInt32
					}
					return int(tmp2) - int(int32(func() int64 {
						if 31 == 1 {
							return ((int64(tmp1) * int64(rc_Q31)) >> 1) + ((int64(tmp1) * int64(rc_Q31)) & 1)
						}
						return (((int64(tmp1) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
					}()))
				}()) * int64(rc_mult2)) >> 1) + ((int64(func() int {
					if ((int(uint32(tmp2)) - int(uint32(int32(func() int64 {
						if 31 == 1 {
							return ((int64(tmp1) * int64(rc_Q31)) >> 1) + ((int64(tmp1) * int64(rc_Q31)) & 1)
						}
						return (((int64(tmp1) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
					}())))) & 0x80000000) == 0 {
						if (int(tmp2) & (int(int32(func() int64 {
							if 31 == 1 {
								return ((int64(tmp1) * int64(rc_Q31)) >> 1) + ((int64(tmp1) * int64(rc_Q31)) & 1)
							}
							return (((int64(tmp1) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
						}())) ^ 0x80000000) & 0x80000000) != 0 {
							return math.MinInt32
						}
						return int(tmp2) - int(int32(func() int64 {
							if 31 == 1 {
								return ((int64(tmp1) * int64(rc_Q31)) >> 1) + ((int64(tmp1) * int64(rc_Q31)) & 1)
							}
							return (((int64(tmp1) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
						}()))
					}
					if ((int(tmp2) ^ 0x80000000) & int(int32(func() int64 {
						if 31 == 1 {
							return ((int64(tmp1) * int64(rc_Q31)) >> 1) + ((int64(tmp1) * int64(rc_Q31)) & 1)
						}
						return (((int64(tmp1) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
					}())) & 0x80000000) != 0 {
						return math.MaxInt32
					}
					return int(tmp2) - int(int32(func() int64 {
						if 31 == 1 {
							return ((int64(tmp1) * int64(rc_Q31)) >> 1) + ((int64(tmp1) * int64(rc_Q31)) & 1)
						}
						return (((int64(tmp1) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
					}()))
				}()) * int64(rc_mult2)) & 1)
			} else {
				tmp64 = (((int64(func() int {
					if ((int(uint32(tmp2)) - int(uint32(int32(func() int64 {
						if 31 == 1 {
							return ((int64(tmp1) * int64(rc_Q31)) >> 1) + ((int64(tmp1) * int64(rc_Q31)) & 1)
						}
						return (((int64(tmp1) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
					}())))) & 0x80000000) == 0 {
						if (int(tmp2) & (int(int32(func() int64 {
							if 31 == 1 {
								return ((int64(tmp1) * int64(rc_Q31)) >> 1) + ((int64(tmp1) * int64(rc_Q31)) & 1)
							}
							return (((int64(tmp1) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
						}())) ^ 0x80000000) & 0x80000000) != 0 {
							return math.MinInt32
						}
						return int(tmp2) - int(int32(func() int64 {
							if 31 == 1 {
								return ((int64(tmp1) * int64(rc_Q31)) >> 1) + ((int64(tmp1) * int64(rc_Q31)) & 1)
							}
							return (((int64(tmp1) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
						}()))
					}
					if ((int(tmp2) ^ 0x80000000) & int(int32(func() int64 {
						if 31 == 1 {
							return ((int64(tmp1) * int64(rc_Q31)) >> 1) + ((int64(tmp1) * int64(rc_Q31)) & 1)
						}
						return (((int64(tmp1) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
					}())) & 0x80000000) != 0 {
						return math.MaxInt32
					}
					return int(tmp2) - int(int32(func() int64 {
						if 31 == 1 {
							return ((int64(tmp1) * int64(rc_Q31)) >> 1) + ((int64(tmp1) * int64(rc_Q31)) & 1)
						}
						return (((int64(tmp1) * int64(rc_Q31)) >> (31 - 1)) + 1) >> 1
					}()))
				}()) * int64(rc_mult2)) >> int64(mult2Q-1)) + 1) >> 1
			}
			if tmp64 > math.MaxInt32 || tmp64 < int64(math.MinInt32) {
				return 0
			}
			A_QA[k-n-1] = int32(tmp64)
		}
	}
	tf := float64(int(1<<QA))*0.99975 + 0.5
	if int(A_QA[k]) > int(int32(tf)) || int(A_QA[k]) < int(-(int32(tf))) {
		return 0
	}
	rc_Q31 = -(int32(int(uint32(A_QA[0])) << (int(31 - QA))))
	rc_mult1_Q30 = int32(int(int32(math.Floor(1*(1<<30)+0.5))) - int(int32((int64(rc_Q31)*int64(rc_Q31))>>32)))
	invGain_Q30 = int32(int(uint32(int32((int64(invGain_Q30)*int64(rc_mult1_Q30))>>32))) << 2)
	if int(invGain_Q30) < int(int32(math.Floor((1.0/MAX_PREDICTION_POWER_GAIN)*(1<<30)+0.5))) {
		return 0
	}
	return invGain_Q30
}
func silk_LPC_inverse_pred_gain_c(A_Q12 []int16, order int) int32 {
	var (
		Atmp_QA [24]int32
		DC_resp int32
	)
	for k := 0; k < order; k++ {
		DC_resp += int32(A_Q12[k])
		Atmp_QA[k] = int32(int(uint32(int32(A_Q12[k]))) << (int(QA - 12)))
	}
	if int(DC_resp) >= 4096 {
		return 0
	}
	return LPC_inverse_pred_gain_QA_c(Atmp_QA, order)
}
