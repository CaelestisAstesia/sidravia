// Package credentialinput defines the character policy for newly supplied
// account/password text. Stored legacy credentials are not input to this rule.
package input

import "errors"

var ErrInvalid = errors.New("账号和密码仅允许英文字母、数字和半角符号，不允许空格、中文、全角字符或控制字符")

// ValidateNewText rejects rather than normalizing text. Callers retain their existing
// required-field and protocol-length rules; this function only checks alphabet.
func ValidateNewText(value string) error {
	for i := 0; i < len(value); i++ {
		if value[i] < 0x21 || value[i] > 0x7e {
			return ErrInvalid
		}
	}
	return nil
}
