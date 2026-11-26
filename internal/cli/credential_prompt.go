package cli

import "sidravia/internal/connection/credential/input"

func (deps authDependencies) readValidatedPassword(fromStdin bool) (string, error) {
	var password string
	var err error
	if fromStdin {
		password, err = deps.readStdinPassword(deps.stdin)
	} else {
		password, err = deps.readInteractivePassword(deps.stdin, deps.stderr)
	}
	if err != nil {
		return "", err
	}
	if err := input.ValidateNewText(password); err != nil {
		return "", err
	}
	return password, nil
}
