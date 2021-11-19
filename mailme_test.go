package mailme

import (
	"testing"

	"gopkg.in/h2non/gock.v1"

	"github.com/nbio/st"
	"github.com/sirupsen/logrus"
)

func TestFetchTemplate(t *testing.T) {
    defer gock.Off()

    var templateBody = "something like QWERTYUIOP."

    gock.New("http://www.example.com").
    Get("/templates/fetch").
    Reply(200).
    BodyString(templateBody)

    var logger = logrus.New()

    var cache = &TemplateCache{
        templates: map[string]*MailTemplate{},
        funcMap: map[string]interface{}{},
        logger: logger,
    }

    data, err := cache.fetchTemplate("http://www.example.com/templates/fetch", 3)

    st.Expect(t, data, templateBody)
    st.Expect(t, err, nil)
}

func TestValidVariables(t *testing.T) {
    var validTemplateBody = "This variable has the correct {{ .myTemplateVariable }} number of braces."

    defer gock.Off()

    gock.New("http://www.example.com").
    Get("/templates/valid").
    Reply(200).
    BodyString(validTemplateBody)
    
    var logger = logrus.New()
    
    var cache = &TemplateCache{
        templates: map[string]*MailTemplate{},
        funcMap: map[string]interface{}{},
        logger: logger,
    }

    data, err := cache.Get("http://www.example.com/templates/valid")

    st.Reject(t, data, nil)
    st.Expect(t, err, nil)
}

func TestInvalidVariable(t *testing.T) {
    var invalidTemplateBody = "This variable has the wrong {{{ .myTemplateVariable }}} number of braces."

    defer gock.Off()

    gock.New("http://www.example.com").
    Get("/templates/invalid").
    Reply(200).
    BodyString(invalidTemplateBody)
    
    var logger = logrus.New()
    
    var cache = &TemplateCache{
        templates: map[string]*MailTemplate{},
        funcMap: map[string]interface{}{},
        logger: logger,
    }

    data, err := cache.Get("http://www.example.com/templates/invalid")

    st.Reject(t, data, invalidTemplateBody)
    st.Reject(t, err, nil)
}