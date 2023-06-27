package drlwe

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tuneinsight/lattigo/v4/ring"
	"github.com/tuneinsight/lattigo/v4/rlwe"
	"github.com/tuneinsight/lattigo/v4/utils/buffer"
	"github.com/tuneinsight/lattigo/v4/utils/sampling"
)

var nbParties = int(5)

var flagParamString = flag.String("params", "", "specify the test cryptographic parameters as a JSON string. Overrides -short and -long.")

func testString(params rlwe.Parameters, level int, opname string) string {
	return fmt.Sprintf("%s/logN=%d/#Qi=%d/#Pi=%d/BitDecomp=%d/NTT=%t/Level=%d/RingType=%s/Parties=%d",
		opname,
		params.LogN(),
		params.QCount(),
		params.PCount(),
		params.Pow2Base(),
		params.NTTFlag(),
		level,
		params.RingType(),
		nbParties)
}

type testContext struct {
	params         rlwe.Parameters
	kgen           *rlwe.KeyGenerator
	skShares       []*rlwe.SecretKey
	skIdeal        *rlwe.SecretKey
	uniformSampler *ring.UniformSampler
	crs            sampling.PRNG
}

func newTestContext(params rlwe.Parameters) *testContext {

	kgen := rlwe.NewKeyGenerator(params)
	skShares := make([]*rlwe.SecretKey, nbParties)
	skIdeal := rlwe.NewSecretKey(params)
	for i := range skShares {
		skShares[i] = kgen.GenSecretKeyNew()
		params.RingQP().Add(skIdeal.Value, skShares[i].Value, skIdeal.Value)
	}

	prng, _ := sampling.NewKeyedPRNG([]byte{'t', 'e', 's', 't'})
	unifSampler := ring.NewUniformSampler(prng, params.RingQ())

	return &testContext{params, kgen, skShares, skIdeal, unifSampler, prng}
}

func (tc testContext) nParties() int {
	return len(tc.skShares)
}

func TestDRLWE(t *testing.T) {

	var err error

	defaultParamsLiteral := testParamsLiteral

	if *flagParamString != "" {
		var jsonParams rlwe.ParametersLiteral
		if err = json.Unmarshal([]byte(*flagParamString), &jsonParams); err != nil {
			t.Fatal(err)
		}
		defaultParamsLiteral = []rlwe.ParametersLiteral{jsonParams} // the custom test suite reads the parameters from the -params flag
	}

	for _, paramsLit := range defaultParamsLiteral {

		for _, NTTFlag := range []bool{true, false} {

			for _, RingType := range []ring.Type{ring.Standard, ring.ConjugateInvariant}[:] {

				paramsLit.NTTFlag = NTTFlag
				paramsLit.RingType = RingType

				var params rlwe.Parameters
				if params, err = rlwe.NewParametersFromLiteral(paramsLit); err != nil {
					t.Fatal(err)
				}

				tc := newTestContext(params)

				testPublicKeyGenProtocol(tc, params.MaxLevel(), t)
				testRelinKeyGenProtocol(tc, params.MaxLevel(), t)
				testEvaluationKeyGenProtocol(tc, params.MaxLevel(), t)
				testGaloisKeyGenProtocol(tc, params.MaxLevel(), t)
				testThreshold(tc, params.MaxLevel(), t)
				testRefreshShare(tc, params.MaxLevel(), t)

				for _, level := range []int{0, params.MaxLevel()} {
					for _, testSet := range []func(tc *testContext, level int, t *testing.T){
						testKeySwitchProtocol,
						testPublicKeySwitchProtocol,
					} {
						testSet(tc, level, t)
						runtime.GC()
					}
				}
			}
		}
	}
}

func testPublicKeyGenProtocol(tc *testContext, level int, t *testing.T) {

	params := tc.params

	t.Run(testString(params, level, "PublicKeyGen/Protocol"), func(t *testing.T) {

		ckg := make([]PublicKeyGenProtocol, nbParties)
		for i := range ckg {
			if i == 0 {
				ckg[i] = NewPublicKeyGenProtocol(params)
			} else {
				ckg[i] = ckg[0].ShallowCopy()
			}
		}

		shares := make([]PublicKeyGenShare, nbParties)
		for i := range shares {
			shares[i] = ckg[i].AllocateShare()
		}

		crp := ckg[0].SampleCRP(tc.crs)

		for i := range shares {
			ckg[i].GenShare(tc.skShares[i], crp, &shares[i])
		}

		for i := 1; i < nbParties; i++ {
			ckg[0].AggregateShares(shares[0], shares[i], &shares[0])
		}

		// Test binary encoding
		buffer.RequireSerializerCorrect(t, &shares[0])

		pk := rlwe.NewPublicKey(params)
		ckg[0].GenPublicKey(shares[0], crp, pk)

		require.True(t, rlwe.PublicKeyIsCorrect(pk, tc.skIdeal, params, math.Log2(math.Sqrt(float64(nbParties))*params.NoiseFreshSK())+1))
	})
}

func testRelinKeyGenProtocol(tc *testContext, level int, t *testing.T) {
	params := tc.params

	t.Run(testString(params, level, "RelinKeyGen/Protocol"), func(t *testing.T) {

		rkg := make([]RelinKeyGenProtocol, nbParties)

		for i := range rkg {
			if i == 0 {
				rkg[i] = NewRelinKeyGenProtocol(params)
			} else {
				rkg[i] = rkg[0].ShallowCopy()
			}
		}

		ephSk := make([]*rlwe.SecretKey, nbParties)
		share1 := make([]RelinKeyGenShare, nbParties)
		share2 := make([]RelinKeyGenShare, nbParties)

		for i := range rkg {
			ephSk[i], share1[i], share2[i] = rkg[i].AllocateShare()
		}

		crp := rkg[0].SampleCRP(tc.crs)
		for i := range rkg {
			rkg[i].GenShareRoundOne(tc.skShares[i], crp, ephSk[i], &share1[i])
		}

		for i := 1; i < nbParties; i++ {
			rkg[0].AggregateShares(share1[0], share1[i], &share1[0])
		}

		// Test binary encoding
		buffer.RequireSerializerCorrect(t, &share1[0])

		for i := range rkg {
			rkg[i].GenShareRoundTwo(ephSk[i], tc.skShares[i], share1[0], &share2[i])
		}

		for i := 1; i < nbParties; i++ {
			rkg[0].AggregateShares(share2[0], share2[i], &share2[0])
		}

		rlk := rlwe.NewRelinearizationKey(params)
		rkg[0].GenRelinearizationKey(share1[0], share2[0], rlk)

		decompRNS := params.DecompRNS(level, params.MaxLevelP())

		noiseBound := math.Log2(math.Sqrt(float64(decompRNS))*NoiseRelinearizationKey(params, nbParties)) + 1

		require.True(t, rlwe.RelinearizationKeyIsCorrect(rlk, tc.skIdeal, params, noiseBound))
	})
}

func testEvaluationKeyGenProtocol(tc *testContext, level int, t *testing.T) {

	params := tc.params

	t.Run(testString(params, level, "EvaluationKeyGen"), func(t *testing.T) {

		evkg := make([]EvaluationKeyGenProtocol, nbParties)
		for i := range evkg {
			if i == 0 {
				evkg[i] = NewEvaluationKeyGenProtocol(params)
			} else {
				evkg[i] = evkg[0].ShallowCopy()
			}
		}

		kgen := rlwe.NewKeyGenerator(params)

		skOutShares := make([]*rlwe.SecretKey, nbParties)
		skOutIdeal := rlwe.NewSecretKey(params)
		for i := range skOutShares {
			skOutShares[i] = kgen.GenSecretKeyNew()
			params.RingQP().Add(skOutIdeal.Value, skOutShares[i].Value, skOutIdeal.Value)
		}

		shares := make([]EvaluationKeyGenShare, nbParties)
		for i := range shares {
			shares[i] = evkg[i].AllocateShare()
		}

		crp := evkg[0].SampleCRP(tc.crs)

		for i := range shares {
			evkg[i].GenShare(tc.skShares[i], skOutShares[i], crp, &shares[i])
		}

		for i := 1; i < nbParties; i++ {
			evkg[0].AggregateShares(shares[0], shares[i], &shares[0])
		}

		// Test binary encoding
		buffer.RequireSerializerCorrect(t, &shares[0])

		evk := rlwe.NewEvaluationKey(params, level, params.MaxLevelP())
		evkg[0].GenEvaluationKey(shares[0], crp, evk)

		decompRNS := params.DecompRNS(level, params.MaxLevelP())

		noiseBound := math.Log2(math.Sqrt(float64(decompRNS))*NoiseEvaluationKey(params, nbParties)) + 1

		require.True(t, rlwe.EvaluationKeyIsCorrect(evk, tc.skIdeal, skOutIdeal, params, noiseBound))
	})
}

func testGaloisKeyGenProtocol(tc *testContext, level int, t *testing.T) {

	params := tc.params

	t.Run(testString(params, level, "GaloisKeyGenProtocol"), func(t *testing.T) {

		gkg := make([]GaloisKeyGenProtocol, nbParties)
		for i := range gkg {
			if i == 0 {
				gkg[i] = NewGaloisKeyGenProtocol(params)
			} else {
				gkg[i] = gkg[0].ShallowCopy()
			}
		}

		shares := make([]GaloisKeyGenShare, nbParties)
		for i := range shares {
			shares[i] = gkg[i].AllocateShare()
		}

		crp := gkg[0].SampleCRP(tc.crs)

		galEl := params.GaloisElement(64)

		for i := range shares {
			gkg[i].GenShare(tc.skShares[i], galEl, crp, &shares[i])
		}

		for i := 1; i < nbParties; i++ {
			gkg[0].AggregateShares(shares[0], shares[i], &shares[0])
		}

		// Test binary encoding
		buffer.RequireSerializerCorrect(t, &shares[0])

		galoisKey := rlwe.NewGaloisKey(params)
		gkg[0].GenGaloisKey(shares[0], crp, galoisKey)

		decompRNS := params.DecompRNS(level, params.MaxLevelP())

		noiseBound := math.Log2(math.Sqrt(float64(decompRNS))*NoiseGaloisKey(params, nbParties)) + 1

		require.True(t, rlwe.GaloisKeyIsCorrect(galoisKey, tc.skIdeal, params, noiseBound))
	})
}

func testKeySwitchProtocol(tc *testContext, level int, t *testing.T) {

	params := tc.params

	t.Run(testString(params, level, "KeySwitch/Protocol"), func(t *testing.T) {

		cks := make([]KeySwitchProtocol, nbParties)

		sigmaSmudging := 8 * rlwe.DefaultNoise

		for i := range cks {
			if i == 0 {
				cks[i] = NewKeySwitchProtocol(params, ring.DiscreteGaussian{Sigma: sigmaSmudging, Bound: 6 * sigmaSmudging})
			} else {
				cks[i] = cks[0].ShallowCopy()
			}
		}

		skout := make([]*rlwe.SecretKey, nbParties)
		skOutIdeal := rlwe.NewSecretKey(params)
		for i := range skout {
			skout[i] = tc.kgen.GenSecretKeyNew()
			params.RingQP().Add(skOutIdeal.Value, skout[i].Value, skOutIdeal.Value)
		}

		ct := rlwe.NewCiphertext(params, 1, level)
		rlwe.NewEncryptor(params, tc.skIdeal).EncryptZero(ct)

		shares := make([]KeySwitchShare, nbParties)
		for i := range shares {
			shares[i] = cks[i].AllocateShare(ct.Level())
		}

		for i := range shares {
			cks[i].GenShare(tc.skShares[i], skout[i], ct, &shares[i])
			if i > 0 {
				cks[0].AggregateShares(shares[0], shares[i], &shares[0])
			}
		}

		// Test binary encoding
		buffer.RequireSerializerCorrect(t, &shares[0])

		ksCt := rlwe.NewCiphertext(params, 1, ct.Level())

		dec := rlwe.NewDecryptor(params, skOutIdeal)

		cks[0].KeySwitch(ct, shares[0], ksCt)

		pt := rlwe.NewPlaintext(params, ct.Level())

		dec.Decrypt(ksCt, pt)

		ringQ := params.RingQ().AtLevel(ct.Level())

		if pt.IsNTT {
			ringQ.INTT(pt.Value, pt.Value)
		}

		require.GreaterOrEqual(t, math.Log2(NoiseKeySwitch(params, nbParties, params.NoiseFreshSK(), float64(sigmaSmudging)))+1, ringQ.Log2OfStandardDeviation(pt.Value))

		cks[0].KeySwitch(ct, shares[0], ct)

		dec.Decrypt(ct, pt)

		if pt.IsNTT {
			ringQ.INTT(pt.Value, pt.Value)
		}

		require.GreaterOrEqual(t, math.Log2(NoiseKeySwitch(params, nbParties, params.NoiseFreshSK(), float64(sigmaSmudging)))+1, ringQ.Log2OfStandardDeviation(pt.Value))
	})
}

func testPublicKeySwitchProtocol(tc *testContext, level int, t *testing.T) {

	params := tc.params

	t.Run(testString(params, level, "PublicKeySwitch/Protocol"), func(t *testing.T) {

		skOut, pkOut := tc.kgen.GenKeyPairNew()

		sigmaSmudging := 8 * rlwe.DefaultNoise

		pcks := make([]PublicKeySwitchProtocol, nbParties)
		for i := range pcks {
			if i == 0 {
				pcks[i] = NewPublicKeySwitchProtocol(params, ring.DiscreteGaussian{Sigma: sigmaSmudging, Bound: 6 * sigmaSmudging})
			} else {
				pcks[i] = pcks[0].ShallowCopy()
			}
		}

		ct := rlwe.NewCiphertext(params, 1, level)

		rlwe.NewEncryptor(params, tc.skIdeal).EncryptZero(ct)

		shares := make([]PublicKeySwitchShare, nbParties)
		for i := range shares {
			shares[i] = pcks[i].AllocateShare(ct.Level())
		}

		for i := range shares {
			pcks[i].GenShare(tc.skShares[i], pkOut, ct, &shares[i])
		}

		for i := 1; i < nbParties; i++ {
			pcks[0].AggregateShares(shares[0], shares[i], &shares[0])
		}

		// Test binary encoding
		buffer.RequireSerializerCorrect(t, &shares[0])

		ksCt := rlwe.NewCiphertext(params, 1, level)
		dec := rlwe.NewDecryptor(params, skOut)

		pcks[0].KeySwitch(ct, shares[0], ksCt)

		pt := rlwe.NewPlaintext(params, ct.Level())
		dec.Decrypt(ksCt, pt)

		ringQ := params.RingQ().AtLevel(ct.Level())

		if pt.IsNTT {
			ringQ.INTT(pt.Value, pt.Value)
		}

		require.GreaterOrEqual(t, math.Log2(NoisePublicKeySwitch(params, nbParties, params.NoiseFreshSK(), float64(sigmaSmudging)))+1, ringQ.Log2OfStandardDeviation(pt.Value))

		pcks[0].KeySwitch(ct, shares[0], ct)

		dec.Decrypt(ct, pt)

		if pt.IsNTT {
			ringQ.INTT(pt.Value, pt.Value)
		}

		require.GreaterOrEqual(t, math.Log2(NoisePublicKeySwitch(params, nbParties, params.NoiseFreshSK(), float64(sigmaSmudging)))+1, ringQ.Log2OfStandardDeviation(pt.Value))
	})
}

func testThreshold(tc *testContext, level int, t *testing.T) {
	sk0Shards := tc.skShares

	for _, threshold := range []int{tc.nParties() / 4, tc.nParties() / 2, tc.nParties() - 1} {
		t.Run(testString(tc.params, level, "Threshold")+fmt.Sprintf("/threshold=%d", threshold), func(t *testing.T) {

			type Party struct {
				Thresholdizer
				Combiner
				gen  ShamirPolynomial
				sk   *rlwe.SecretKey
				tsks ShamirSecretShare
				tsk  *rlwe.SecretKey
				tpk  ShamirPublicPoint
			}

			P := make([]*Party, tc.nParties())
			shamirPks := make([]ShamirPublicPoint, tc.nParties())
			for i := 0; i < tc.nParties(); i++ {
				p := new(Party)
				p.Thresholdizer = NewThresholdizer(tc.params)
				p.sk = sk0Shards[i]
				p.tsk = rlwe.NewSecretKey(tc.params)
				p.tpk = ShamirPublicPoint(i + 1)
				p.tsks = p.Thresholdizer.AllocateThresholdSecretShare()
				P[i] = p
				shamirPks[i] = p.tpk
			}

			for _, pi := range P {
				pi.Combiner = NewCombiner(tc.params, pi.tpk, shamirPks, threshold)
			}

			shares := make(map[*Party]map[*Party]ShamirSecretShare, tc.nParties())
			var err error
			// Every party generates a share for every other party
			for _, pi := range P {

				pi.gen, err = pi.Thresholdizer.GenShamirPolynomial(threshold, pi.sk)
				if err != nil {
					t.Error(err)
				}

				shares[pi] = make(map[*Party]ShamirSecretShare)
				for _, pj := range P {
					shares[pi][pj] = pi.Thresholdizer.AllocateThresholdSecretShare()
					share := shares[pi][pj]
					pi.Thresholdizer.GenShamirSecretShare(pj.tpk, pi.gen, &share)
				}
			}

			//Each party aggregates what it has received into a secret key
			for _, pi := range P {
				for _, pj := range P {
					share := shares[pj][pi]
					pi.Thresholdizer.AggregateShares(pi.tsks, share, &pi.tsks)
				}
			}

			// Test binary encoding
			buffer.RequireSerializerCorrect(t, &P[0].tsks)

			// Determining which parties are active. In a distributed context, a party
			// would receive the ids of active players and retrieve (or compute) the corresponding keys.
			activeParties := P[:threshold]
			activeShamirPks := make([]ShamirPublicPoint, threshold)
			for i, p := range activeParties {
				activeShamirPks[i] = p.tpk
			}

			// Combining
			// Slow because each party has to generate its public key on-the-fly. In
			// practice the public key could be precomputed from an id by parties during setup
			ringQP := tc.params.RingQP()
			recSk := rlwe.NewSecretKey(tc.params)
			for _, pi := range activeParties {
				pi.Combiner.GenAdditiveShare(activeShamirPks, pi.tpk, pi.tsks, pi.tsk)
				ringQP.Add(pi.tsk.Value, recSk.Value, recSk.Value)
			}

			require.True(t, tc.skIdeal.Equal(recSk)) // reconstructed key should match the ideal sk
		})
	}
}

func testRefreshShare(tc *testContext, level int, t *testing.T) {
	t.Run(testString(tc.params, level, "RefreshShare"), func(t *testing.T) {
		params := tc.params
		ringQ := params.RingQ().AtLevel(level)
		ciphertext := &rlwe.Ciphertext{}
		ciphertext.Value = []ring.Poly{{}, ringQ.NewPoly()}
		tc.uniformSampler.AtLevel(level).Read(ciphertext.Value[1])
		cksp := NewKeySwitchProtocol(tc.params, tc.params.Xe())
		share1 := cksp.AllocateShare(level)
		share2 := cksp.AllocateShare(level)
		cksp.GenShare(tc.skShares[0], tc.skShares[1], ciphertext, &share1)
		cksp.GenShare(tc.skShares[1], tc.skShares[0], ciphertext, &share2)
		buffer.RequireSerializerCorrect(t, &RefreshShare{EncToShareShare: share1, ShareToEncShare: share2})
	})
}
