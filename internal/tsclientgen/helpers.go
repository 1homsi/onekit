package tsclientgen

import "github.com/1homsi/onekit/internal/tscommon"

// snakeToLowerCamel converts "user_id" to "userId".
func snakeToLowerCamel(s string) string {
	return tscommon.SnakeToLowerCamel(s)
}

// headerNameToPropertyName converts "X-API-Key" to "apiKey".
func headerNameToPropertyName(headerName string) string {
	return tscommon.HeaderNameToPropertyName(headerName)
}
