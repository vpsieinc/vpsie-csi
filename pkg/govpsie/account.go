package govpsie

import (
	"context"
	"net/http"
)

type AccountService interface {
	Login(ctx context.Context, loginCredentials *LoginReq) (*Token, error)
}

type accountServiceHandler struct {
	client *Client
}

var _ AccountService = &accountServiceHandler{}

type LoginReq struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
}

type Token struct {
	Access  TokenDetails `json:"access"`
	Refresh TokenDetails `json:"refresh"`
}

type TokenDetails struct {
	Token   string `json:"token"`
	Expires string `json:"expires"`
}

type TokenRoot struct {
	Error bool  `json:"error"`
	Token Token `json:"token"`
}

func (a *accountServiceHandler) Login(ctx context.Context, loginCredentials *LoginReq) (*Token, error) {
	req, err := a.client.NewRequest(ctx, http.MethodPost, "/apps/v2/auth/from/api", loginCredentials)
	if err != nil {
		return nil, err
	}

	token := new(TokenRoot)
	if err = a.client.Do(ctx, req, token); err != nil {
		return nil, err
	}

	return &token.Token, nil
}
