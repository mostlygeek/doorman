package doorman

import (
	"net/http"

	jwt "gopkg.in/square/go-jose.v2/jwt"
)

// Claims is the set of information we extract from the JWT payload.
type Claims struct {
	Subject  string       `json:"sub,omitempty"`
	Audience jwt.Audience `json:"aud,omitempty"`
	Email    string       `json:"email,omitempty"`
	Groups   []string     `json:"groups,omitempty"`
}

// JWTValidator is the interface in charge of extracting JWT claims from request.
type JWTValidator interface {
	Initialize() error
	ExtractClaims(*http.Request) (*Claims, error)
}

var jwtValidators map[string]JWTValidator

func init() {
	jwtValidators = map[string]JWTValidator{}
}

// NewJWTValidator instantiates a JWT validator for the specified issuer.
func NewJWTValidator(issuer string) (JWTValidator, error) {
	// Reuse JWT validators instances among configs if they are for the same issuer.
	v, ok := jwtValidators[issuer]
	if !ok {
		// XXX: currently only Auth0 is supported.
		v = &Auth0Validator{
			Issuer: issuer,
		}
		err := v.Initialize()
		if err != nil {
			return nil, err
		}
	}
	return v, nil
}
