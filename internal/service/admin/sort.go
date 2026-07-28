package admin

func ProductSort(value string) string {
	return allowedSort(value, productSorts, "created_at DESC, id DESC")
}

func OrderSort(value string) string {
	return allowedSort(value, orderSorts, "o.created_at DESC, o.id DESC")
}

func CustomerSort(value string) string {
	return allowedSort(value, customerSorts, "u.id DESC")
}

func allowedSort(value string, allowed map[string]string, fallback string) string {
	if result, ok := allowed[value]; ok {
		return result
	}
	return fallback
}

var productSorts = map[string]string{
	"name": "name ASC, id ASC", "price": "price ASC, id ASC", "stock": "stock ASC, id ASC", "created_at": "created_at ASC, id ASC",
}

var orderSorts = map[string]string{
	"created_at": "o.created_at ASC, o.id ASC", "total": "o.total DESC, o.id DESC",
}

var customerSorts = map[string]string{
	"email": "u.email ASC, u.id ASC", "name": "u.last_name ASC, u.first_name ASC, u.id ASC",
}
