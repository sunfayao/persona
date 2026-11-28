package transcript

import (
	"bufio"
	"errors"
	"io"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/sunfayao/persona/server/internal/proto"
)

func compressedTranscript(path string) bool { return strings.HasSuffix(path, ".jsonl.zst") }

type compressedStream struct {
	*zstd.Decoder
	source io.Closer
}

func (s *compressedStream) Close() error {
	s.Decoder.Close()
	return s.source.Close()
}

// OpenStream reads a plain transcript or a compressed rollout without materializing it on disk.
func OpenStream(path string) (io.ReadCloser, error) {
	source, err := openTranscript(path)
	if err != nil {
		return nil, err
	}
	if !compressedTranscript(path) {
		return source, nil
	}
	decoder, err := zstd.NewReader(source, zstd.WithDecoderConcurrency(1))
	if err != nil {
		_ = source.Close()
		return nil, err
	}
	return &compressedStream{Decoder: decoder, source: source}, nil
}

func (f *File) refreshCompressed() ([]proto.Item, Span, error) {
	st, err := statTranscript(f.Path)
	if err != nil {
		return nil, Span{}, err
	}
	if !st.Mode().IsRegular() {
		return nil, Span{}, errors.New("transcript source is not a regular file")
	}
	if unchanged(st, f.offset, f.ino) && st.ModTime().Equal(f.readMtime) {
		return nil, Span{f.history(), f.lines, f.lines}, nil
	}
	stream, err := OpenStream(f.Path)
	if err != nil {
		return nil, Span{}, err
	}
	defer stream.Close()
	next := NewParsedFile(f.Path, f.sources)
	reader := bufio.NewReaderSize(stream, 64<<10)
	var fresh []proto.Item
	var offset int64
	for {
		line, err := reader.ReadBytes('\n')
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, Span{}, err
		}
		next.appendLine(line[:len(line)-1], offset, &fresh)
		offset += int64(len(line))
	}
	if !f.readMtime.IsZero() {
		f.gen++
	}
	f.offset, f.ino, f.readMtime = st.Size(), inode(st), st.ModTime()
	f.lines, f.items, f.refs, f.meta, f.tree = next.lines, next.items, next.refs, next.meta, next.tree
	f.firstHash = next.firstHash
	f.base, f.win, f.partialMeta, f.partial = 0, nil, false, nil
	return fresh, Span{History: f.history(), To: f.lines}, nil
}

func readTranscriptLine(path string, ref lineRef) ([]byte, error) {
	line := make([]byte, ref.n)
	if compressedTranscript(path) {
		stream, err := OpenStream(path)
		if err != nil {
			return nil, err
		}
		defer stream.Close()
		if _, err := io.CopyN(io.Discard, stream, ref.off); err != nil {
			return nil, err
		}
		_, err = io.ReadFull(stream, line)
		return line, err
	}
	file, err := openTranscript(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	_, err = file.ReadAt(line, ref.off)
	return line, err
}
