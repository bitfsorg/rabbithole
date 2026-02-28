package buy

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tongxiaofeng/bitfs/internal/client"
)

func TestCatResponse_TextContent(t *testing.T) {
	resp := CatResponse{
		Meta:    &client.MetaResponse{PNode: "02ab", Path: "/readme.txt", MimeType: "text/plain", FileSize: 11, Access: "free"},
		Content: strPtr("hello world"),
	}
	data, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"content":"hello world"`)
	assert.NotContains(t, string(data), "content_base64")
}

func TestCatResponse_BinaryContent(t *testing.T) {
	resp := CatResponse{
		Meta:          &client.MetaResponse{PNode: "02ab", Path: "/img.jpg", MimeType: "image/jpeg", FileSize: 3, Access: "free"},
		ContentBase64: strPtr("AQID"),
	}
	data, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"content_base64":"AQID"`)
	assert.NotContains(t, string(data), `"content":`)
}

func TestCatResponse_PaymentRequired(t *testing.T) {
	resp := CatResponse{
		Meta:            &client.MetaResponse{PNode: "02ab", Path: "/secret.txt", Access: "paid", PricePerKB: 100},
		PaymentRequired: true,
		PaymentInfo:     &PaymentInfo{Price: 1000, PricePerKB: 100, SellerPubKey: "03ff", PaymentAddr: "aabb"},
	}
	data, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"payment_required":true`)
	assert.Contains(t, string(data), `"price":1000`)
}

func TestGetResponse_Success(t *testing.T) {
	resp := GetResponse{
		Meta:         &client.MetaResponse{PNode: "02ab", Path: "/file.bin", FileSize: 100, Access: "free"},
		OutputPath:   "/tmp/file.bin",
		BytesWritten: 100,
	}
	data, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"output_path":"/tmp/file.bin"`)
	assert.Contains(t, string(data), `"bytes_written":100`)
}

func TestErrorResponse(t *testing.T) {
	resp := ErrorResponse{Error: "not found", Code: 2}
	data, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.JSONEq(t, `{"error":"not found","code":2}`, string(data))
}

func strPtr(s string) *string { return &s }
