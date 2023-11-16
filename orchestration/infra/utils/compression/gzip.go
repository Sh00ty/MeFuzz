// Package compression - данный пакет пока что не используется в проекте (добавится позже)
package compression

import (
	"bytes"
	"compress/zlib"
	"io"
	"orchestration/infra/utils/logger"
)

const threshold = 1024

func Compress(data []byte) ([]byte, bool, error) {
	if len(data) < threshold {
		return data, false, nil
	}
	var buf bytes.Buffer

	gWriter, err := zlib.NewWriterLevel(&buf, zlib.BestCompression)
	if err != nil {
		return nil, false, err
	}
	if _, err := gWriter.Write(data); err != nil {
		return nil, false, err
	}
	if err = gWriter.Close(); err != nil {
		return nil, false, err
	}
	return buf.Bytes(), true, nil
}

func DeCompress(data []byte) ([]byte, error) {
	gReader, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err = gReader.Close(); err != nil {
			logger.Errorf(err, "failed to close gzip reader")
		}
	}()
	return io.ReadAll(gReader)
}
