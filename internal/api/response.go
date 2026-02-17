package api

type SuccessResponse struct {
	Data any `json:"data"`
}

type ListResponse struct {
	Data   any   `json:"data"`
	Total  int64 `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
