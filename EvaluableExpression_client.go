package govaluate

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// ToClientExpression Returns a string representing this expression in a simplified and client compatible way
func (expression EvaluableExpression) ToClientExpression(parameters map[string]interface{}) (string, error) {

	var stream *tokenStream
	var transactions *expressionOutputStream
	var transaction string
	var err error

	transactions = new(expressionOutputStream)

	ret := expression.traverseStages(expression.evaluationStages, parameters)
	stream = newTokenStream(ret)
	for stream.hasNext() {

		transaction, err = expression.findNextClientString(stream, transactions, parameters)
		if err != nil {
			return "", err
		}

		transactions.add(transaction)
	}

	return transactions.createString(" "), nil
}

// Traverse the simplified tree and return the expression tokens in order
func (expression EvaluableExpression) traverseStages(rootStage *evaluationStage, parameters map[string]interface{}) []ExpressionToken {
	ret := []ExpressionToken{}

	if rootStage != nil {

		if rootStage.symbol == NOOP {
			clause := ExpressionToken{
				Kind: CLAUSE,
			}
			ret = append(ret, clause)
		}

		left := expression.traverseStages(rootStage.leftStage, parameters)
		ret = append(ret, left...)

		if rootStage.originalToken.Value != nil {
			ret = append(ret, rootStage.originalToken)
		}

		right := expression.traverseStages(rootStage.rightStage, parameters)
		ret = append(ret, right...)
		if rootStage.symbol == NOOP {
			clause := ExpressionToken{
				Kind: CLAUSE_CLOSE,
			}
			ret = append(ret, clause)
		}
		ret = simplifyTokens(ret, parameters)

	}
	return ret
}

func simplifyTokens(tokens []ExpressionToken, parameters map[string]interface{}) []ExpressionToken {
	if len(tokens) > 2 {
		if new, err := NewEvaluableExpressionFromTokens(tokens); err == nil {
			val, err := new.Evaluate(parameters)
			if err == nil {
				if newTokens, err := parseTokens(fmt.Sprintf("%v", val), nil); err == nil {
					tokens = newTokens
				}
			}
		}
	}
	return tokens
}

func (expression EvaluableExpression) findNextClientString(stream *tokenStream, transactions *expressionOutputStream, parameters map[string]interface{}) (string, error) {

	var token ExpressionToken
	var ret string

	token = stream.next()

	switch token.Kind {

	case PATTERN:
		ret = fmt.Sprintf("'%s'", token.Value.(*regexp.Regexp).String())
	case TIME:
		ret = fmt.Sprintf("'%s'", token.Value.(time.Time).Format(expression.QueryDateFormat))

	case BOOLEAN:
		if token.Value.(bool) {
			ret = "TRUE"
		} else {
			ret = "FALSE"
		}

	case LOGICALOP, COMPARATOR, STRING:
		ret = fmt.Sprintf("%v", token.Value)

	case VARIABLE:
		variable := token.Value.(string)
		if val, ok := parameters[variable]; ok {
			ret = fmt.Sprintf("%v", val)
		} else {
			ret = fmt.Sprintf("%s", variable)
		}
	case NUMERIC:
		ret = fmt.Sprintf("%g", token.Value.(float64))

	case TERNARY:

		switch ternarySymbols[token.Value.(string)] {

		case COALESCE:

			left := transactions.rollback()
			right, err := expression.findNextClientString(stream, transactions, parameters)
			if err != nil {
				return "", err
			}

			ret = fmt.Sprintf("IF(%v == NULL, %v, %v)", left, right, left)
		case TERNARY_TRUE:
			left := transactions.rollback()
			ret = fmt.Sprintf("%v", left)
		case TERNARY_FALSE:
			return "", errors.New("Ternary operators are unsupported in SQL output")

		}
	case PREFIX:
		switch prefixSymbols[token.Value.(string)] {

		case INVERT:
			nextToken := stream.next()
			stream.rewind()

			right, err := expression.findNextClientString(stream, transactions, parameters)
			if err != nil {
				return "", err
			}

			if nextToken.Kind == CLAUSE {
				ret = fmt.Sprintf("NOT %s", right)
			} else {
				ret = fmt.Sprintf("NOT (%s)", right)
			}

		default:

			right, err := expression.findNextClientString(stream, transactions, parameters)
			if err != nil {
				return "", err
			}

			ret = fmt.Sprintf("%s%s", token.Value.(string), right)
		}
	case MODIFIER:

		switch modifierSymbols[token.Value.(string)] {

		case EXPONENT:

			left := transactions.rollback()
			right, err := expression.findNextClientString(stream, transactions, parameters)
			if err != nil {
				return "", err
			}

			ret = fmt.Sprintf("%s^%s", left, right)
		case MODULUS:

			left := transactions.rollback()
			right, err := expression.findNextClientString(stream, transactions, parameters)
			if err != nil {
				return "", err
			}

			ret = fmt.Sprintf("%s %% %s", left, right)
		default:
			ret = fmt.Sprintf("%s", token.Value.(string))
		}
	case CLAUSE:
		ret = "("
	case CLAUSE_CLOSE:
		ret = ")"
	case SEPARATOR:
		ret = ","

	default:
		errorMsg := fmt.Sprintf("Unrecognized query token '%s' of kind '%s'", token.Value, token.Kind)
		return "", errors.New(errorMsg)
	}

	return ret, nil
}
