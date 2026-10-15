package bridge

// CMYK 软打样（票 72 / ADR-0008）。

// 文档像素始终是 RGB。校样是视图变换：把活动配置的 33³ 查找表交给前端，
// 由画布在显示时采样。查找表不进 manifest，撤销也不认识它。

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"

	"compositor-win/internal/colorio"
)

// cmykProfileReply is one installed profile, without its bytes.
type cmykProfileReply struct {
	Name   string `json:"name"`
	Chosen bool   `json:"chosen"`
}

// CMYKProfiles lists the CMYK profiles the app can proof with.
func (s *Service) CMYKProfiles() (string, error) {
	chosen := colorio.Chosen()
	out := make([]cmykProfileReply, 0)
	for _, p := range colorio.Installed() {
		out = append(out, cmykProfileReply{
			Name:   p.Name,
			Chosen: chosen != nil && chosen.Name == p.Name,
		})
	}
	data, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SetCMYKProfile remembers which installed profile proofs. An empty name
// clears the choice.
func (s *Service) SetCMYKProfile(name string) error {
	return colorio.SetChosen(name)
}

// LoadCMYKProfile copies an ICC file into the app's profile folder and
// selects it. The path comes from the file dialog the frontend already ran.
func (s *Service) LoadCMYKProfile(path string) (string, error) {
	p, err := colorio.Load(path)
	if err != nil {
		return "", err
	}
	if err := colorio.SetChosen(p.Name); err != nil {
		return "", err
	}
	return p.Name, nil
}

// cmykProofReply is the three lookups the canvas samples, base64 so the
// bridge's string reply can carry the float32 bytes without a binary IPC.
type cmykProofReply struct {
	Dimension int    `json:"dimension"`
	Profile   string `json:"profile"`
	Proof     string `json:"proof"`
	Warning   string `json:"warning"`
	Marked    string `json:"marked"`
}

// CMYKProofLookups builds the proof lookups for the chosen profile, or the
// generic one when nothing is chosen. No CMYK profile at all is an error the
// menu can show; it is not a reason to invent a proof.
func (s *Service) CMYKProofLookups() (string, error) {
	profile := colorio.Chosen()
	if profile == nil {
		if generic, ok := colorio.Generic(); ok {
			profile = &generic
		}
	}
	if profile == nil {
		return "", fmt.Errorf("没有可用的 CMYK 配置。请先在视图菜单中载入一个 ICC 配置。")
	}
	luts, err := colorio.ProofLookups(profile.Data)
	if err != nil {
		return "", err
	}
	reply := cmykProofReply{
		Dimension: colorio.ProofDimension,
		Profile:   profile.Name,
		Proof:     base64.StdEncoding.EncodeToString(float32Bytes(luts.Proof)),
		Warning:   base64.StdEncoding.EncodeToString(float32Bytes(luts.Warning)),
		Marked:    base64.StdEncoding.EncodeToString(float32Bytes(luts.Marked)),
	}
	data, err := json.Marshal(reply)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func float32Bytes(values []float32) []byte {
	out := make([]byte, len(values)*4)
	for i, v := range values {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(v))
	}
	return out
}
