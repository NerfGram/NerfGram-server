package domain

import "errors"

// 文件分片上传与媒体相关业务错误。rpc 层据此映射为对应 rpc_error（见 internal/rpc/errors.go）：
// 文件分片（part invalid/parts invalid/part too big）、上传配额、照片与文档。
var (
	ErrFilePartInvalid     = errors.New("file part invalid")
	ErrFilePartsInvalid    = errors.New("file parts invalid")
	ErrFilePartTooBig      = errors.New("file part too big")
	ErrUploadQuotaExceeded = errors.New("upload quota exceeded")
	// ErrStorageFull means the configured storage capacity guard rejected a
	// write. It is distinct from per-account upload quota and must not be mapped
	// to FLOOD_WAIT: retrying cannot create disk or object-store capacity.
	ErrStorageFull     = errors.New("storage full")
	ErrPhotoInvalid    = errors.New("photo invalid")
	ErrDocumentInvalid = errors.New("document invalid")
)
