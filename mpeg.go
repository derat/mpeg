// Copyright 2022 Daniel Erat.
// All rights reserved.

// Package mpeg returns information about MPEG (and specifically MP3) audio files.
package mpeg

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/derat/taglib-go/taglib"
	"github.com/derat/taglib-go/taglib/id3"
)

// ID3v1Tag contains information from an ID3v1 footer at the end of an MP3 file.
// ID3v1 is a terrible format: https://id3.org/ID3v1
type ID3v1Tag struct {
	Title, Artist, Album, Year, Comment string
	Genre, Track                        byte
}

// ID3v1Length is the length in bytes of an ID3v1 tag.
const ID3v1Length = 128

// ReadID3v1Footer reads an ID3v1 footer from the final ID3v1Length bytes of f.
// If the tag isn't present, the returned tag and error will be nil.
func ReadID3v1Footer(f *os.File, fi os.FileInfo) (*ID3v1Tag, error) {
	const (
		footerMagic = "TAG"
		titleLen    = 30
		artistLen   = 30
		albumLen    = 30
		yearLen     = 4
		commentLen  = 30
		genreLen    = 1
	)

	// Check for an ID3v1 footer.
	buf := make([]byte, ID3v1Length)
	if _, err := f.ReadAt(buf, fi.Size()-int64(len(buf))); err != nil {
		return nil, err
	}
	b := bytes.NewBuffer(buf)
	if string(b.Next(len(footerMagic))) != footerMagic {
		return nil, nil
	}

	clean := func(b []byte) string { return string(bytes.TrimSpace(bytes.TrimRight(b, "\x00"))) }

	tag := &ID3v1Tag{}
	tag.Title = clean(b.Next(titleLen))
	tag.Artist = clean(b.Next(artistLen))
	tag.Album = clean(b.Next(albumLen))
	tag.Year = clean(b.Next(yearLen))
	comment := b.Next(commentLen)
	tag.Genre = b.Next(genreLen)[0]

	// ID3v1.1 extension: if the last byte of the comment field is non-zero but the byte before it
	// is zero, then the last byte holds the track number.
	idx1, idx2 := len(comment)-1, len(comment)-2
	if comment[idx1] != 0x0 && comment[idx2] == 0x0 {
		tag.Track = comment[idx1]
		comment[idx1] = 0x0
	}
	tag.Comment = clean(comment)

	return tag, nil
}

// GetID3v2TextFrame returns the first ID3v2 text frame with the supplied ID from gen.
// If the frame isn't present, an empty string and nil error are returned.
//
// The taglib library has built-in support for some frames ("TPE1", "TIT2", "TALB", etc.)
// and provides generic support for custom "TXXX" frames, but it doesn't seem to provide
// an easy way to read other well-known frames like "TPE2".
func GetID3v2TextFrame(gen taglib.GenericTag, id string) (string, error) {
	switch tag := gen.(type) {
	case *id3.Id3v23Tag:
		if frames := tag.Frames[id]; len(frames) == 0 {
			return "", nil
		} else if fields, err := id3.GetId3v23TextIdentificationFrame(frames[0]); err != nil {
			return "", err
		} else {
			return fields[0], nil
		}
	case *id3.Id3v24Tag:
		if frames := tag.Frames[id]; len(frames) == 0 {
			return "", nil
		} else if fields, err := id3.GetId3v24TextIdentificationFrame(frames[0]); err != nil {
			return "", err
		} else {
			return fields[0], nil
		}
	default:
		return "", errors.New("unsupported ID3 version")
	}
}

// ComputeAudioSHA1 returns a SHA1 hash of the audio (i.e. non-metadata) portion of f.
func ComputeAudioSHA1(f *os.File, fi os.FileInfo, headerLen, footerLen int64) (string, error) {
	if _, err := f.Seek(headerLen, 0); err != nil {
		return "", err
	}
	hasher := sha1.New()
	if _, err := io.CopyN(hasher, f, fi.Size()-headerLen-footerLen); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// FrameInfo contains information about an MPEG (MP3?) audio frame header.
type FrameInfo struct {
	KbitRate        int // in 1000 bits per second (not 1024)
	SampleRate      int // in hertz
	SamplesPerFrame int
	ChannelMode     uint8 // 0x0 stereo, 0x1 joint stereo, 0x2 dual channel, 0x3 single channel
	HasCRC          bool  // 16-bit CRC follows header
	HasPadding      bool  // frame is padded with one extra bit
}

func (fi *FrameInfo) Size() int64 {
	// See https://www.opennet.ru/docs/formats/mpeghdr.html. Calculation may be more complicated per
	// https://www.codeproject.com/Articles/8295/MPEG-Audio-Frame-Header, but if we're off we'll
	// probably see a problem when reading the next frame.
	s := int64(fi.SamplesPerFrame/8) * int64(fi.KbitRate*1000) / int64(fi.SampleRate)
	if fi.HasPadding {
		s++
	}
	return s
}

func (fi *FrameInfo) Empty() bool {
	// TODO: This seems bogus.
	return fi.Size() == 104
}

type version int

const (
	version1 version = iota
	version2
	version2_5 // unofficial extension of MPEG2
	versionRes // reserved
)

type layer int

const (
	layer1 layer = iota
	layer2
	layer3
	layerRes // reserved
)

var versions = [...]version{version2_5, versionRes, version2, version1}
var layers = [...]layer{layerRes, layer3, layer2, layer1}

// Specific to Layer III.
var samplesPerFrame = map[version]int{
	version1:   1152,
	version2:   576,
	version2_5: 576,
}

// Specific to Layer III. Values are multiples of 1000 bits.
var kbitRates = map[version][]int{
	version1:   {0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0},
	version2:   {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0},
	version2_5: {0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}, // same as version2
}

// Values are in Hertz.
var sampleRates = map[version][]int{
	version1:   {44100, 48000, 32000, 0},
	version2:   {22050, 24000, 16000, 0},
	version2_5: {11025, 12000, 8000, 0},
}

var unsupportedLayerErr = errors.New("unsupported layer")

// ReadFrameInfo reads an MPEG audio frame header at the specified offset in f.
// Format details at http://www.codeproject.com/Articles/8295/MPEG-Audio-Frame-Header.
func ReadFrameInfo(f *os.File, start int64) (*FrameInfo, error) {
	if _, err := f.Seek(start, 0); err != nil {
		return nil, err
	}
	var header uint32
	if err := binary.Read(f, binary.BigEndian, &header); err != nil {
		return nil, err
	}
	getBits := func(startBit, numBits uint) uint32 {
		return (header << startBit) >> (32 - numBits)
	}
	if sync := getBits(0, 11); sync != 0x7ff {
		return nil, errors.New("no 0x7ff sync")
	}
	version := versions[getBits(11, 2)]
	if version == versionRes {
		return nil, errors.New("invalid MPEG version")
	}
	if layer := layers[getBits(13, 2)]; layer != layer3 {
		return nil, unsupportedLayerErr
	}

	finfo := FrameInfo{
		KbitRate:        kbitRates[version][getBits(16, 4)],
		SampleRate:      sampleRates[version][getBits(20, 2)],
		SamplesPerFrame: samplesPerFrame[version],
		ChannelMode:     uint8(getBits(24, 2)),
		HasCRC:          getBits(15, 1) == 0x0,
		HasPadding:      getBits(22, 1) == 0x1,
	}
	if finfo.KbitRate == 0 {
		return nil, errors.New("invalid bitrate")
	} else if finfo.SampleRate == 0 {
		return nil, errors.New("invalid sampling rate")
	}
	return &finfo, nil
}

// I've seen some files that seemed to have a bunch of junk (or at least not an MPEG header
// starting with sync bits) after the header offset identified by taglib-go. Scan up to this
// many bytes to try to find something that looks like a proper header.
const maxFrameSearchBytes = 8192

// ComputeAudioDuration reads an Xing header from the frame at headerLen in f to return the audio length.
// If no Xing header is present, it assumes that the file has a constant bitrate and returns a nil
// VBRInfo struct. Only supports MPEG Audio 1, Layer 3.
// TODO: Consider adding support for VBRI headers, apparently only writte by the Fraunhofer
// encoder: https://www.codeproject.com/Articles/8295/MPEG-Audio-Frame-Header#VBRIHeader
func ComputeAudioDuration(f *os.File, fi os.FileInfo, headerLen, footerLen int64) (time.Duration, *VBRInfo, error) {
	// Scan forward in case there's empty space or other junk before the first frame.
	var finfo *FrameInfo
	var err error
	fstart := headerLen
	for ; fstart < headerLen+maxFrameSearchBytes; fstart++ {
		if finfo, err = ReadFrameInfo(f, fstart); err == nil {
			break
		} else if err == unsupportedLayerErr {
			return 0, nil, err
		}
	}
	if err != nil {
		return 0, nil, fmt.Errorf("didn't find header after %#x", headerLen)
	}

	// Figure out where the Xing header should start.
	xingStart := fstart + 4
	if finfo.ChannelMode == 0x3 { // mono
		xingStart += 17
	} else {
		xingStart += 32
	}
	if finfo.HasCRC {
		xingStart += 2
	}
	if _, err := f.Seek(xingStart, io.SeekStart); err != nil {
		return 0, nil, fmt.Errorf("seek to Xing header at %#x: %v", xingStart, err)
	}

	// Read 4-byte ID at beginning of header.
	id := make([]byte, 4)
	if _, err := f.Read(id); err != nil {
		return 0, nil, err
	}
	if VBRHeaderID(id) != XingID && VBRHeaderID(id) != InfoID {
		// Okay, no Xing VBR header. Assume that the file has a fixed bitrate.
		// (The other alternative is to read the whole file to count the number of frames.)
		ms := (fi.Size() - fstart - footerLen) / int64(finfo.KbitRate) * 8
		return time.Duration(ms) * time.Millisecond, nil, nil
	}
	vbrInfo := VBRInfo{ID: VBRHeaderID(id)}

	// Read 4-byte flags indicating which fields are present.
	var flags uint32
	if err := binary.Read(f, binary.BigEndian, &flags); err != nil {
		return 0, nil, err
	}

	// Read 4-byte frame count. This is optional in the spec, but we require it since it's
	// needed to compute the duration.
	if flags&0x1 == 0 {
		return 0, nil, errors.New("Xing header lacks number of frames")
	}
	if err := binary.Read(f, binary.BigEndian, &vbrInfo.Frames); err != nil {
		return 0, nil, err
	}

	// Read 4-byte byte count if present.
	if flags&0x2 != 0 {
		if err := binary.Read(f, binary.BigEndian, &vbrInfo.Bytes); err != nil {
			return 0, nil, err
		}
	}

	// Skip 100-byte TOC if present.
	if flags&0x3 != 0 {
		if _, err := f.Seek(100, io.SeekCurrent); err != nil {
			return 0, nil, err
		}
	}

	// Read 4-byte quality indicator if present.
	if flags&0x4 != 0 {
		var quality uint32
		if err := binary.Read(f, binary.BigEndian, &quality); err != nil {
			return 0, nil, err
		}
		vbrInfo.Quality = int(quality)
	}

	// Try to read the beginning of the LAME extension:
	// http://gabriel.mp3-tech.org/mp3infotag.html
	b := make([]byte, 10)
	if _, err := f.Read(b); err == nil {
		enc := b[:9]
		ver := (b[9] & 0xf0) >> 4
		if (ver == 0 || ver == 1) && isEncoderString(enc) {
			vbrInfo.Encoder = strings.TrimSpace(string(enc))
			vbrInfo.Method = EncodingMethod(b[9] & 0xf)
		}
	}

	ms := int64(finfo.SamplesPerFrame) * int64(vbrInfo.Frames) * 1000 / int64(finfo.SampleRate)
	return time.Duration(ms) * time.Millisecond, &vbrInfo, nil
}

// isEncoderString returns true if b contains only printable characters.
func isEncoderString(b []byte) bool {
	for _, ch := range b {
		if !strconv.IsPrint(rune(ch)) {
			return false
		}
	}
	return true
}

// VBRInfo contains information from an Xing (or Info) header in the first frame.
// See https://www.codeproject.com/Articles/8295/MPEG-Audio-Frame-Header#XINGHeader.
type VBRInfo struct {
	// ID contains the ID from the beginning of the header.
	ID VBRHeaderID
	// Frames contains the number of audio frames in the file.
	Frames uint32
	// Bytes contains the number of bytes of audio data in the file.
	Bytes uint32
	// Quality contains a poorly-defined quality indicator in the range [0, 100].
	Quality int
	// Encoder describes the encoder version, e.g. "LAME3.90a".
	Encoder string
	// Method describes how the audio was encoded.
	Method EncodingMethod
}

// VBRHeaderID describes the type of header used to fill a VBRInfo.
type VBRHeaderID string

const (
	// XingID typically indicates a VBR or ABR stream.
	XingID VBRHeaderID = "Xing"
	// InfoID typically indicates a CBR stream.
	InfoID VBRHeaderID = "Info"
)

// EncodingMethod describes the encoding method used for the file.
type EncodingMethod int

const (
	UnknownMethod EncodingMethod = 0
	CBR           EncodingMethod = 1
	ABR           EncodingMethod = 2
	VBR1          EncodingMethod = 3 // Lame: VBR old / VBR RH
	VBR2          EncodingMethod = 4 // Lame: VBR MTRH
	VBR3          EncodingMethod = 5 // LAME: VBR MT
	VBR4          EncodingMethod = 6
	CBR2Pass      EncodingMethod = 8
	ABR2Pass      EncodingMethod = 9
)

var encMethodNames = map[EncodingMethod]string{
	UnknownMethod: "unknown",
	CBR:           "CBR",
	ABR:           "ABR",
	VBR1:          "VBR1",
	VBR2:          "VBR2",
	VBR3:          "VBR3",
	VBR4:          "VBR4",
	CBR2Pass:      "CBR 2-pass",
	ABR2Pass:      "ABR 2-pass",
}

func (m EncodingMethod) String() string {
	if s, ok := encMethodNames[m]; ok {
		return s
	}
	return fmt.Sprintf("invalid (%d)", int(m))
}
