package evaluation

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"sort"
)

// FingerprintDocuments 计算与输入顺序无关、对索引正文敏感的语料指纹。
func FingerprintDocuments(documents []Document) string {
	ordered := append([]Document(nil), documents...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	digest := sha256.New()
	for _, document := range ordered {
		writeFingerprintField(digest, document.ID)
		writeFingerprintField(digest, document.StoredName)
		writeFingerprintField(digest, document.Content)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func writeFingerprintField(digest hash.Hash, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write([]byte(value))
}
