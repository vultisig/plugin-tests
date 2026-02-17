package api

type SuccessResponse struct {
	Data interface{} `json:"data"`
}

type ListResponse struct {
	Data   interface{} `json:"data"`
	Total  int64       `json:"total"`
	Limit  int         `json:"limit"`
	Offset int         `json:"offset"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
