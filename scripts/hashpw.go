// hashpw is a small helper to generate a bcrypt password hash.
// Usage:  go run scripts/hashpw.go yourpassword
// Copy the output and paste it into the admins table.
package main

import (
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run scripts/hashpw.go <password>")
		os.Exit(1)
	}

	password := os.Args[1]

	// bcrypt.DefaultCost = 10, a good balance of security and speed
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}

	fmt.Println(string(hash))
}
