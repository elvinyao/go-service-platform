package dataaccess

type DataAccessor interface {
	GetData(key string) (interface{}, error)
	SetData(key string, value interface{}) error
}
