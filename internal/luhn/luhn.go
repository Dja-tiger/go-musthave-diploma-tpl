// Package luhn validates numeric strings with the Luhn checksum algorithm.
package luhn

// Valid reports whether number consists only of digits and has a valid Luhn
// checksum.
func Valid(number string) bool {
	if number == "" {
		return false
	}

	sum := 0
	double := false

	for i := len(number) - 1; i >= 0; i-- {
		ch := number[i]
		if ch < '0' || ch > '9' {
			return false
		}

		digit := int(ch - '0')
		if double {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}

		sum += digit
		double = !double
	}

	return sum%10 == 0
}
