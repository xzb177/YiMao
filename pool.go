package main

import (
	"bytes"
	"strings"
	"sync"
)

// JSON buffer pool for reducing allocations during JSON marshaling
var jsonBufferPool = sync.Pool{
	New: func() interface{} {
		return new(bytes.Buffer)
	},
}

// getJSONBuffer gets a buffer from pool
func getJSONBuffer() *bytes.Buffer {
	return jsonBufferPool.Get().(*bytes.Buffer)
}

// putJSONBuffer returns a buffer to pool after reset
func putJSONBuffer(buf *bytes.Buffer) {
	if buf.Cap() > 1024*1024 { // Don't pool buffers larger than 1MB
		return
	}
	buf.Reset()
	jsonBufferPool.Put(buf)
}

// StringBuilderPool for efficient string concatenation
var stringBuilderPool = sync.Pool{
	New: func() interface{} {
		return &strings.Builder{}
	},
}

func getStringBuilder() *strings.Builder {
	return stringBuilderPool.Get().(*strings.Builder)
}

func putStringBuilder(sb *strings.Builder) {
	if sb.Cap() > 1024*64 { // Don't pool builders larger than 64KB
		return
	}
	sb.Reset()
	stringBuilderPool.Put(sb)
}
