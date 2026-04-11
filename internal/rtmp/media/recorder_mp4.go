package media

import (
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	"github.com/alxayo/go-rtmp/internal/rtmp/chunk"
)

// MP4Recorder writes RTMP media to MP4 format, streaming frames directly
// to disk to avoid buffering the entire recording in memory.
//
// File layout: [ftyp box][mdat box (streamed)][moov box (written on Close)]
//
// On creation, the ftyp box and mdat header (with placeholder size) are written.
// Each WriteMessage() appends frame data to mdat on disk immediately.
// On Close(), the mdat header is patched with the actual size, and the moov
// box (track metadata + sample tables) is appended at the end.
//
// Memory usage is O(number_of_samples) for sample metadata only — frame data
// is never held in memory. A 1-hour recording at 30fps uses ~3MB of metadata
// vs ~2GB if frames were buffered.
type MP4Recorder struct {
	mu            sync.Mutex
	file          *os.File
	logger        *slog.Logger
	disabled      bool
	samples       []mp4Sample
	mdatStart     int64  // file offset where mdat box begins
	mdatDataSize  int64  // bytes of frame data written to mdat
	lastTimestamp uint32
}

type mp4Sample struct {
	isVideo   bool
	offset    int64  // offset within mdat data (relative to mdat payload start)
	size      int32
	timestamp uint32
}

const (
	ftypBoxSize = 32 // fixed ftyp box size
	mdatHdrSize = 8  // mdat box header (size + "mdat")
)

func NewMP4Recorder(path string, logger *slog.Logger) (MediaWriter, error) {
	if logger == nil {
		logger = slog.Default()
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("mp4_recorder.create: %w", err)
	}

	r := &MP4Recorder{
		file:      f,
		logger:    logger,
		samples:   make([]mp4Sample, 0, 1024),
		mdatStart: ftypBoxSize,
	}

	// Write ftyp box immediately
	if err := r.writeFtypBox(); err != nil {
		f.Close()
		return nil, fmt.Errorf("mp4_recorder.ftyp: %w", err)
	}

	// Write mdat header with placeholder size (patched on Close)
	var mdatHdr [mdatHdrSize]byte
	copy(mdatHdr[4:], []byte("mdat"))
	if _, err := f.Write(mdatHdr[:]); err != nil {
		f.Close()
		return nil, fmt.Errorf("mp4_recorder.mdat_header: %w", err)
	}

	return r, nil
}

func (r *MP4Recorder) WriteMessage(msg *chunk.Message) {
	if msg == nil || (msg.TypeID != 8 && msg.TypeID != 9) {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.disabled {
		return
	}

	isVideo := msg.TypeID == 9
	offset := r.mdatDataSize
	size := int32(len(msg.Payload))

	// Stream frame data directly to disk
	if _, err := r.file.Write(msg.Payload); err != nil {
		r.logger.Error("mp4_recorder write failed", "err", err)
		r.disabled = true
		return
	}

	r.samples = append(r.samples, mp4Sample{
		isVideo:   isVideo,
		offset:    offset,
		size:      size,
		timestamp: msg.Timestamp,
	})
	r.mdatDataSize += int64(size)
	r.lastTimestamp = msg.Timestamp
}

func (r *MP4Recorder) Disabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.disabled
}

func (r *MP4Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.file == nil {
		return nil
	}
	defer func() {
		r.file.Close()
		r.file = nil
	}()

	if r.disabled || len(r.samples) == 0 {
		return nil
	}

	// Patch mdat box size: seek to mdat header, write actual size
	mdatBoxSize := uint32(mdatHdrSize + r.mdatDataSize)
	var sizeBuf [4]byte
	binary.BigEndian.PutUint32(sizeBuf[:], mdatBoxSize)
	if _, err := r.file.WriteAt(sizeBuf[:], r.mdatStart); err != nil {
		return fmt.Errorf("mp4_recorder.patch_mdat: %w", err)
	}

	// Append moov box at end of file
	if err := r.writeMoovBox(r.file); err != nil {
		return fmt.Errorf("mp4_recorder.moov: %w", err)
	}

	return nil
}

func (r *MP4Recorder) writeFtypBox() error {
	ftypData := []byte{
		0x00, 0x00, 0x00, 0x20, 'f', 't', 'y', 'p',
		'i', 's', 'o', 'm', 0x00, 0x00, 0x00, 0x00,
		'i', 's', 'o', 'm', 'i', 's', 'o', '2',
		'a', 'v', 'c', '1', 'h', 'e', 'v', '1',
	}
	_, err := r.file.Write(ftypData)
	return err
}

func (r *MP4Recorder) writeMoovBox(w io.Writer) error {
	moovBuf := newMP4BoxBuilder()

	mvhdBuf := newMP4BoxBuilder()
	mvhdBuf.writeU32(0)
	mvhdBuf.writeU32(1000)
	mvhdBuf.writeU32(1000)
	mvhdBuf.writeU32(1000)
	mvhdBuf.writeU32(r.lastTimestamp)
	mvhdBuf.writeU32(0x00010000)
	mvhdBuf.writeU16(0x0100)
	mvhdBuf.writeBytes(make([]byte, 10))
	mvhdBuf.writeBytes(identityMatrix())
	mvhdBuf.writeU32(2)
	mvhdBuf.writeU32(2)
	mvhdBuf.writeU32(2)
	moovBuf.writeBox("mvhd", mvhdBuf.Bytes())

	if r.hasVideoSamples() {
		trakBuf := r.buildVideoTrack()
		moovBuf.writeBox("trak", trakBuf.Bytes())
	}

	if r.hasAudioSamples() {
		trakBuf := r.buildAudioTrack()
		moovBuf.writeBox("trak", trakBuf.Bytes())
	}

	moovData := moovBuf.Bytes()
	return r.writeBoxHeader(w, "moov", moovData)
}

func (r *MP4Recorder) buildVideoTrack() *mp4BoxBuilder {
	trakBuf := newMP4BoxBuilder()

	tkhdBuf := newMP4BoxBuilder()
	tkhdBuf.writeU32(0xf)
	tkhdBuf.writeU32(1000)
	tkhdBuf.writeU32(1000)
	tkhdBuf.writeU32(1)
	tkhdBuf.writeU32(0)
	tkhdBuf.writeU32(r.lastTimestamp)
	tkhdBuf.writeBytes(make([]byte, 8))
	tkhdBuf.writeU16(0)
	tkhdBuf.writeU16(0)
	tkhdBuf.writeU16(0x0100)
	tkhdBuf.writeBytes(make([]byte, 2))
	tkhdBuf.writeBytes(identityMatrix())
	tkhdBuf.writeU32(0x04b00000)
	tkhdBuf.writeU32(0x02d00000)
	trakBuf.writeBox("tkhd", tkhdBuf.Bytes())

	mediaBuf := newMP4BoxBuilder()
	mdhdBuf := newMP4BoxBuilder()
	mdhdBuf.writeU32(0)
	mdhdBuf.writeU32(1000)
	mdhdBuf.writeU32(1000)
	mdhdBuf.writeU32(1000)
	mdhdBuf.writeU32(r.lastTimestamp)
	mdhdBuf.writeU16(0x55c4)
	mdhdBuf.writeU16(0)
	mediaBuf.writeBox("mdhd", mdhdBuf.Bytes())

	hdlrBuf := newMP4BoxBuilder()
	hdlrBuf.writeU32(0)
	hdlrBuf.writeU32(0)
	hdlrBuf.writeBytes([]byte("vide"))
	hdlrBuf.writeBytes(make([]byte, 12))
	hdlrBuf.writeBytes([]byte("Video Handler\x00"))
	mediaBuf.writeBox("hdlr", hdlrBuf.Bytes())

	minfBuf := newMP4BoxBuilder()
	vmhdBuf := newMP4BoxBuilder()
	vmhdBuf.writeU32(0)
	vmhdBuf.writeU16(0)
	vmhdBuf.writeU16(0)
	vmhdBuf.writeU16(0)
	vmhdBuf.writeU16(0)
	minfBuf.writeBox("vmhd", vmhdBuf.Bytes())

	dinfBuf := newMP4BoxBuilder()
	drefBuf := newMP4BoxBuilder()
	drefBuf.writeU32(0)
	drefBuf.writeU32(1)
	urleBuf := newMP4BoxBuilder()
	urleBuf.writeU32(1)
	drefBuf.writeBox("url ", urleBuf.Bytes())
	dinfBuf.writeBox("dref", drefBuf.Bytes())
	minfBuf.writeBox("dinf", dinfBuf.Bytes())

	stblBuf := r.buildSampleTable(true)
	minfBuf.writeBox("stbl", stblBuf.Bytes())
	mediaBuf.writeBox("minf", minfBuf.Bytes())
	trakBuf.writeBox("mdia", mediaBuf.Bytes())

	return trakBuf
}

func (r *MP4Recorder) buildAudioTrack() *mp4BoxBuilder {
	trakBuf := newMP4BoxBuilder()

	tkhdBuf := newMP4BoxBuilder()
	tkhdBuf.writeU32(0xf)
	tkhdBuf.writeU32(1000)
	tkhdBuf.writeU32(1000)
	tkhdBuf.writeU32(2)
	tkhdBuf.writeU32(0)
	tkhdBuf.writeU32(r.lastTimestamp)
	tkhdBuf.writeBytes(make([]byte, 8))
	tkhdBuf.writeU16(0)
	tkhdBuf.writeU16(0)
	tkhdBuf.writeU16(0x0100)
	tkhdBuf.writeBytes(make([]byte, 2))
	tkhdBuf.writeBytes(identityMatrix())
	tkhdBuf.writeU32(0)
	tkhdBuf.writeU32(0)
	trakBuf.writeBox("tkhd", tkhdBuf.Bytes())

	mediaBuf := newMP4BoxBuilder()
	mdhdBuf := newMP4BoxBuilder()
	mdhdBuf.writeU32(0)
	mdhdBuf.writeU32(1000)
	mdhdBuf.writeU32(1000)
	mdhdBuf.writeU32(48000)
	audioSamples := (r.lastTimestamp * 48) / 1000
	mdhdBuf.writeU32(audioSamples)
	mdhdBuf.writeU16(0x55c4)
	mdhdBuf.writeU16(0)
	mediaBuf.writeBox("mdhd", mdhdBuf.Bytes())

	hdlrBuf := newMP4BoxBuilder()
	hdlrBuf.writeU32(0)
	hdlrBuf.writeU32(0)
	hdlrBuf.writeBytes([]byte("soun"))
	hdlrBuf.writeBytes(make([]byte, 12))
	hdlrBuf.writeBytes([]byte("Sound Handler\x00"))
	mediaBuf.writeBox("hdlr", hdlrBuf.Bytes())

	minfBuf := newMP4BoxBuilder()
	smhdBuf := newMP4BoxBuilder()
	smhdBuf.writeU32(0)
	smhdBuf.writeU16(0)
	smhdBuf.writeU16(0)
	minfBuf.writeBox("smhd", smhdBuf.Bytes())

	dinfBuf := newMP4BoxBuilder()
	drefBuf := newMP4BoxBuilder()
	drefBuf.writeU32(0)
	drefBuf.writeU32(1)
	urleBuf := newMP4BoxBuilder()
	urleBuf.writeU32(1)
	drefBuf.writeBox("url ", urleBuf.Bytes())
	dinfBuf.writeBox("dref", drefBuf.Bytes())
	minfBuf.writeBox("dinf", dinfBuf.Bytes())

	stblBuf := r.buildSampleTable(false)
	minfBuf.writeBox("stbl", stblBuf.Bytes())
	mediaBuf.writeBox("minf", minfBuf.Bytes())
	trakBuf.writeBox("mdia", mediaBuf.Bytes())

	return trakBuf
}

func (r *MP4Recorder) buildSampleTable(isVideo bool) *mp4BoxBuilder {
	stblBuf := newMP4BoxBuilder()

	stsdBuf := newMP4BoxBuilder()
	stsdBuf.writeU32(0)
	stsdBuf.writeU32(1)

	if isVideo {
		sampleBuf := newMP4BoxBuilder()
		sampleBuf.writeBytes(make([]byte, 6))
		sampleBuf.writeU16(1)
		sampleBuf.writeU16(0)
		sampleBuf.writeU16(0)
		sampleBuf.writeBytes(make([]byte, 12))
		sampleBuf.writeU16(1280)
		sampleBuf.writeU16(720)
		sampleBuf.writeU32(0x00480000)
		sampleBuf.writeU32(0x00480000)
		sampleBuf.writeU32(0)
		sampleBuf.writeU16(1)
		sampleBuf.writeBytes(make([]byte, 32))
		sampleBuf.writeU16(0x0018)
		sampleBuf.writeU16(0xffff)

		hvcCBuf := newMP4BoxBuilder()
		hvcCBuf.writeU8(0)
		hvcCBuf.writeU8(0)
		hvcCBuf.writeU32(0)
		hvcCBuf.writeU32(0)
		hvcCBuf.writeU16(0)
		hvcCBuf.writeU8(0)
		sampleBuf.writeBox("hvcC", hvcCBuf.Bytes())

		stsdBuf.writeBox("hvc1", sampleBuf.Bytes())
	} else {
		sampleBuf := newMP4BoxBuilder()
		sampleBuf.writeBytes(make([]byte, 6))
		sampleBuf.writeU16(1)
		sampleBuf.writeU16(0)
		sampleBuf.writeU16(0)
		sampleBuf.writeU32(0)
		sampleBuf.writeU16(1)
		sampleBuf.writeU16(16)
		sampleBuf.writeU16(0)
		sampleBuf.writeU16(0)
		sampleBuf.writeU32(0x0c000000)

		esdsBuf := newMP4BoxBuilder()
		esdsBuf.writeU32(0)
		esdsBuf.writeU8(0x03)
		esdsBuf.writeU8(0x19)
		esdsBuf.writeU16(0)
		esdsBuf.writeU8(0)
		esdsBuf.writeU8(0x04)
		esdsBuf.writeU8(0x11)
		esdsBuf.writeU8(0x40)
		esdsBuf.writeU8(0x15)
		esdsBuf.writeU32(0x1000)
		esdsBuf.writeU32(0x0002)
		esdsBuf.writeU8(0x05)
		esdsBuf.writeU8(0x02)
		esdsBuf.writeU16(0x1188)
		esdsBuf.writeU8(0x06)
		esdsBuf.writeU8(0x01)
		esdsBuf.writeU8(0x02)
		sampleBuf.writeBox("esds", esdsBuf.Bytes())

		stsdBuf.writeBox("mp4a", sampleBuf.Bytes())
	}

	stblBuf.writeBox("stsd", stsdBuf.Bytes())

	sttsBuf := newMP4BoxBuilder()
	sttsBuf.writeU32(0)
	sttsBuf.writeU32(1)
	sttsBuf.writeU32(uint32(len(r.samples)))
	sttsBuf.writeU32(33)
	stblBuf.writeBox("stts", sttsBuf.Bytes())

	stscBuf := newMP4BoxBuilder()
	stscBuf.writeU32(0)
	stscBuf.writeU32(1)
	stscBuf.writeU32(1)
	stscBuf.writeU32(uint32(len(r.samples)))
	stscBuf.writeU32(1)
	stblBuf.writeBox("stsc", stscBuf.Bytes())

	stsz := newMP4BoxBuilder()
	stsz.writeU32(0)
	stsz.writeU32(0)
	stsz.writeU32(uint32(len(r.samples)))
	for i := range r.samples {
		stsz.writeU32(uint32(r.samples[i].size))
	}
	stblBuf.writeBox("stsz", stsz.Bytes())

	// stco: chunk offset — points to start of mdat payload
	stcoBuf := newMP4BoxBuilder()
	stcoBuf.writeU32(0)
	stcoBuf.writeU32(1)
	stcoBuf.writeU32(uint32(ftypBoxSize + mdatHdrSize)) // offset to first byte of mdat data
	stblBuf.writeBox("stco", stcoBuf.Bytes())

	return stblBuf
}

func (r *MP4Recorder) hasVideoSamples() bool {
	for i := range r.samples {
		if r.samples[i].isVideo {
			return true
		}
	}
	return false
}

func (r *MP4Recorder) hasAudioSamples() bool {
	for i := range r.samples {
		if !r.samples[i].isVideo {
			return true
		}
	}
	return false
}

func (r *MP4Recorder) writeBoxHeader(w io.Writer, boxType string, data []byte) error {
	var header [8]byte
	binary.BigEndian.PutUint32(header[:], uint32(8+len(data)))
	copy(header[4:], []byte(boxType))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

type mp4BoxBuilder struct {
	data []byte
}

func newMP4BoxBuilder() *mp4BoxBuilder {
	return &mp4BoxBuilder{data: make([]byte, 0, 1024)}
}

func (b *mp4BoxBuilder) Bytes() []byte {
	return b.data
}

func (b *mp4BoxBuilder) writeU8(v uint8) {
	b.data = append(b.data, v)
}

func (b *mp4BoxBuilder) writeU16(v uint16) {
	var buf [2]byte
	binary.BigEndian.PutUint16(buf[:], v)
	b.data = append(b.data, buf[:]...)
}

func (b *mp4BoxBuilder) writeU32(v uint32) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], v)
	b.data = append(b.data, buf[:]...)
}

func (b *mp4BoxBuilder) writeBytes(p []byte) {
	b.data = append(b.data, p...)
}

func (b *mp4BoxBuilder) writeBox(boxType string, contents []byte) {
	var header [8]byte
	binary.BigEndian.PutUint32(header[:], uint32(8+len(contents)))
	copy(header[4:], []byte(boxType))
	b.data = append(b.data, header[:]...)
	b.data = append(b.data, contents...)
}

func identityMatrix() []byte {
	return []byte{
		0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x40, 0x00, 0x00, 0x00,
	}
}
