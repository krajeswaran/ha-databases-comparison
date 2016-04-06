package model

type Balance struct {
	UserId     string `json:"userId"`
	Balance    int64  `json:"balance"` // actual balance field
	BalanceRef string `json:""`        // reference to balance field counter
	// ..some extra fields in the real world
}
