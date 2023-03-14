package driver

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"code.k9.ms/vpsie-csi/pkg/govpsie"

	"golang.org/x/oauth2"
)

type tknSource struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

const tokenFile = "token.json"

type TokenData struct {
	Token          string
	ExpirationTime time.Time
}

func (tk *tknSource) Token() (*oauth2.Token, error) {
	tokenData, err := tk.ReadTokenData()
	if err != nil {
		return nil, err
	}

	tokenData, err = tk.ReadTokenAndExpirationTime(tokenData)
	if err != nil {
		return nil, err
	}

	return &oauth2.Token{
		AccessToken: tokenData.Token,
		TokenType:   "Bearer",
	}, nil

}

func (tk *tknSource) ReadTokenData() (*TokenData, error) {
	tokenData := TokenData{}
	file, err := os.Open(tokenFile)
	if err != nil {
		if os.IsNotExist(err) {

			return tk.GenerateNewToken()
		}
		panic(err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&tokenData); err != nil {
		panic(err)
	}

	return &tokenData, nil
}

func (tk *tknSource) SaveTokenData(tokenData *TokenData) error {
	file, err := os.Create(tokenFile)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	if err := encoder.Encode(tokenData); err != nil {
		return err
	}
	return nil
}

func (tk *tknSource) ReadTokenAndExpirationTime(tokenData *TokenData) (*TokenData, error) {
	currentTime := time.Now().UTC()
	if tokenData.ExpirationTime.Before(currentTime) {
		var err error
		tokenData, err = tk.GenerateNewToken()
		if err != nil {
			return nil, err
		}
		return tokenData, nil
	}
	return tokenData, nil
}

func (tk *tknSource) GenerateNewToken() (*TokenData, error) {
	// code to generate a new token
	client := govpsie.NewClient(http.DefaultClient)

	credentials := govpsie.LoginReq{
		ClientID:     tk.ClientID,
		ClientSecret: tk.ClientSecret,
	}

	token, err := client.Account.Login(context.Background(), &credentials)
	if err != nil {
		return nil, err
	}

	t, err := time.Parse(time.RFC3339, token.Access.Expires)
	if err != nil {
		return nil, err
	}

	tokenData := &TokenData{
		Token:          token.Access.Token,
		ExpirationTime: t,
	}

	err = tk.SaveTokenData(tokenData)
	if err != nil {
		return nil, err
	}

	return tokenData, nil
}
