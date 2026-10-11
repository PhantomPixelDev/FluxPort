package web

import (
	"fmt"
	"sort"
	"strings"

	"fluxport/internal/collections"
	"fluxport/internal/conversion"
	"github.com/gofiber/fiber/v3"
)

type PipelineParameters struct { Field,Value,From,To,Direction,Keys,Aggregate,Alias,JoinID,Kind string }
func firstMapKey[T any](m map[string]T) string {keys:=[]string{};for key:=range m{keys=append(keys,key)};sort.Strings(keys);if len(keys)==0{return ""};return keys[0]}
func pipelineParameters(step collections.PipelineStep) PipelineParameters {
	p:=PipelineParameters{Direction:"asc",Aggregate:"count",Kind:"eq"}
	switch step.Op{
	case "filter":if len(step.Filters)>0{f:=step.Filters[0];p.Field=f.Field;p.Kind=f.Op;b,_:=conversion.MarshalJSON(f.Value);p.Value=string(b)}
	case "map":p.Field=firstMapKey(step.Set);if p.Field!=""{b,_:=conversion.MarshalJSON(step.Set[p.Field]);p.Value=string(b)}
	case "rename":p.From=firstMapKey(step.Rules.Rename);p.To=step.Rules.Rename[p.From]
	case "select":p.Keys=strings.Join(step.Rules.Select,", ")
	case "drop":p.Keys=strings.Join(step.Rules.Drop,", ")
	case "sort":if len(step.Sort)>0{p.Field=step.Sort[0].Field;if step.Sort[0].Desc{p.Direction="desc"}}
	case "deduplicate":p.Keys=strings.Join(step.Keys,", ")
	case "group","aggregate":p.Keys=strings.Join(step.Keys,", ");if len(step.Aggregates)>0{a:=step.Aggregates[0];p.Aggregate=a.Op;p.Field=a.Field;p.Alias=a.As}
	case "join":p.Kind="left";if j:=step.Join;j!=nil{p.JoinID=j.Collection;p.From=j.Left;p.To=j.Right;p.Field=j.As;p.Kind=j.Kind}
	}
	return p
}
func commaFields(text string) []string {fields:=[]string{};for _,field:=range strings.Split(text,","){field=strings.TrimSpace(field);if field!=""{fields=append(fields,field)}};return fields}
func applyPipelineParameters(c fiber.Ctx,steps []collections.PipelineStep) error {
	ids:=c.Request().PostArgs().PeekMulti("step_id");if len(ids)==0{return nil};if len(ids)!=len(steps){return fmt.Errorf("step controls do not match the recipe")}
	for i,id:=range ids{key:=string(id);if len(key)>64{return fmt.Errorf("invalid step controls")};prefix:="param_"+key+"_";if c.FormValue(prefix+"changed")!="1"{continue};get:=func(name string)string{return c.FormValue(prefix+name)};step:=&steps[i]
		switch step.Op{
		case "filter":value,err:=conversion.Decode([]byte(get("value")),"json");if get("kind")=="null"||get("kind")=="notnull"{value=nil;err=nil};if err!=nil{return fmt.Errorf("step %d value: %w",i+1,err)};f:=collections.Filter{Field:get("field"),Op:get("kind"),Value:value};if len(step.Filters)==0{step.Filters=append(step.Filters,f)}else{step.Filters[0]=f}
		case "map":value,err:=conversion.Decode([]byte(get("value")),"json");if err!=nil{return fmt.Errorf("step %d value: %w",i+1,err)};if step.Set==nil{step.Set=map[string]any{}};delete(step.Set,firstMapKey(step.Set));step.Set[get("field")]=value
		case "rename":if step.Rules.Rename==nil{step.Rules.Rename=map[string]string{}};delete(step.Rules.Rename,firstMapKey(step.Rules.Rename));step.Rules.Rename[get("from")]=get("to")
		case "select":step.Rules.Select=commaFields(get("keys"))
		case "drop":step.Rules.Drop=commaFields(get("keys"))
		case "sort":s:=collections.Sort{Field:get("field"),Desc:get("direction")=="desc"};if len(step.Sort)==0{step.Sort=append(step.Sort,s)}else{step.Sort[0]=s}
		case "deduplicate":step.Keys=commaFields(get("keys"))
		case "group","aggregate":step.Keys=commaFields(get("keys"));a:=collections.Aggregate{Op:get("aggregate"),Field:get("field"),As:get("alias")};if len(step.Aggregates)==0{step.Aggregates=append(step.Aggregates,a)}else{step.Aggregates[0]=a}
		case "join":step.Join=&collections.PipelineJoin{Collection:get("join"),Left:get("from"),Right:get("to"),As:get("field"),Kind:get("kind")}
		}
	}
	return nil
}
