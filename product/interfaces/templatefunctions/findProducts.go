package templatefunctions

import (
	"context"
	"log"
	"strconv"

	"flamingo.me/flamingo-commerce/v3/product/application"
	searchApplication "flamingo.me/flamingo-commerce/v3/search/application"
	"flamingo.me/pugtemplate/pugjs"
)

// FindProducts is exported as a template function
type FindProducts struct {
	SearchService *application.ProductSearchService `inject:""`
}

// Func defines the find products function
func (tf *FindProducts) Func(ctx context.Context) interface{} {

	/*
		widgetName - used to namespace widget - in case we need pagination
		config - map with certain keys - used to specifiy th searchRequest better
	*/
	return func(widgetName string, searchConfig interface{}, additionalFilters interface{}) *application.SearchResult {
		var searchRequest searchApplication.SearchRequest
		//fmt.Printf("%#v", searchConfig)

		if pugjsMap, ok := searchConfig.(*pugjs.Map); ok {
			searchConfigValues := pugjsMap.AsStringMap()
			//fmt.Printf("%#v", searchConfigValues)

			searchRequest = searchApplication.SearchRequest{
				SortDirection: searchConfigValues["sortDirection"],
				SortBy:        searchConfigValues["sortBy"],
				Query:         searchConfigValues["query"],
			}
			pageSize, err := strconv.Atoi(searchConfigValues["pageSize"])
			if err == nil {
				searchRequest.PageSize = pageSize
			}
		}

		searchRequest.FilterBy = asFilterMap(additionalFilters)
		//fmt.Printf("%#v", searchRequest)
		result, e := tf.SearchService.Find(ctx, &searchRequest)
		if e != nil {
			log.Printf("Error: product.interfaces.templatefunc %v", e)
			return &application.SearchResult{}
		}

		return result
	}
}

func asFilterMap(additionalFilters interface{}) map[string]interface{} {
	filters := make(map[string]interface{})
	// use filtersPug as KeyValueFilter
	if filtersPug, ok := additionalFilters.(*pugjs.Map); ok {
		for k, v := range filtersPug.AsStringIfaceMap() {
			if v, ok := v.([]pugjs.Object); ok {
				var filterList []string
				for _, item := range v {
					filterList = append(filterList, item.String())
				}
				filters[k] = filterList
			}
			if v, ok := v.(pugjs.String); ok {
				filters[k] = []string{v.String()}
			}
		}
	}
	return filters
}
