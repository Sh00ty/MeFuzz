// Package compression - данный пакет пока что не используется в проекте (добавится позже)
package compression

import (
	"bytes"
	"compress/zlib"
	"io"
	"orchestration/infra/utils/logger"
)

const threshold = 1024

func Compress(data []byte) ([]byte, error) {
	if len(data) < threshold {
		return data, nil
	}
	var buf bytes.Buffer
	gWriter, err := zlib.NewWriterLevel(&buf, zlib.BestSpeed)
	if err != nil {
		return nil, err
	}
	if _, err := gWriter.Write(data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DeCompress(data []byte) ([]byte, error) {
	gReader, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := gReader.Close(); err != nil {
			logger.Errorf(err, "failed to close gzip reader")
		}
	}()
	return io.ReadAll(gReader)
}
