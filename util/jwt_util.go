package util

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func MakeJWTAuthHttpHandlerFunc(f http.HandlerFunc) http.HandlerFunc {

	return func(w http.ResponseWriter, r *http.Request) {

		JwtToken := r.Header.Get("Auth")

		_, err := ValidateJwtToken(JwtToken)

		if err != nil {

			WriteJSON(w, 403, map[string]string{"error": "invalid jwt token"})
			return
		}

		f(w, r)
	}
}

func GenerateJwtToken(username string) (string, *HTTPError) {

	secretKey := os.Getenv("JWT_PRIVATE_KEY")

	claims := &jwt.RegisteredClaims{
		Subject:  username,
		IssuedAt: jwt.NewNumericDate(time.Now()),
	}
	log.Println(secretKey)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenString, err := token.SignedString([]byte(secretKey))

	if err != nil {
		log.Println(err.Error())
		return "", &HTTPError{Error: "error while generating jwt token.", Status: 500}
	}

	return tokenString, nil
}

func ValidateJwtToken(JwtToken string) (*jwt.Token, error) {

	secretKey := os.Getenv("JWT_PRIVATE_KEY")
	log.Println("the token received is: " + JwtToken)

	return jwt.Parse(JwtToken, func(token *jwt.Token) (interface{}, error) {

		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {

			return nil, fmt.Errorf("wrong signing key")
		}

		return []byte(secretKey), nil
	})
}

func GetUsernameFromJwtToken(JwtToken string) (string, error) {

	secretKey := os.Getenv("JWT_PRIVATE_KEY")

	parsedToken, err := jwt.Parse(JwtToken, func(token *jwt.Token) (interface{}, error) {

		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {

			return nil, fmt.Errorf("wrong signing key")
		}

		return []byte(secretKey), nil
	})

	if err != nil {

		return "", fmt.Errorf("error in parsing JWT")
	}
	claims, ok := parsedToken.Claims.(jwt.MapClaims)

	if !ok {
		return "", fmt.Errorf("error in getting claims from JWT")
	}

	username, ok := claims["sub"].(string)
	if !ok {
		return "", fmt.Errorf("error in extracting subject claim from JWT")

	}

	return username, nil

}
