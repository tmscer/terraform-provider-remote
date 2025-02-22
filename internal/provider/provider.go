package provider

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func init() {
	// Set descriptions to support markdown syntax, this will be used in document generation
	// and the language server.
	schema.DescriptionKind = schema.StringMarkdown

	// Customize the content of descriptions when output. For example you can add defaults on
	// to the exported descriptions if present.
	schema.SchemaDescriptionBuilder = func(s *schema.Schema) string {
		desc := s.Description
		if s.Default != nil {
			desc += fmt.Sprintf(" Defaults to `%v`.", s.Default)
		}
		return strings.TrimSpace(desc)
	}
}

func New(version string) func() *schema.Provider {
	return func() *schema.Provider {
		p := &schema.Provider{
			DataSourcesMap: map[string]*schema.Resource{
				"remote_file": dataSourceRemoteFile(),
			},
			ResourcesMap: map[string]*schema.Resource{
				"remote_file": resourceRemoteFile(),
			},
			Schema: map[string]*schema.Schema{
				"conn": {
					Type:        schema.TypeList,
					MinItems:    0,
					MaxItems:    1,
					Optional:    true,
					Description: "Default connection to host where files are located. Can be overridden in resources and data sources.",
					Elem:        connectionSchemaResource,
				},
				"proxy_conn": {
					Type:        schema.TypeList,
					MinItems:    0,
					MaxItems:    1,
					Optional:    true,
					Description: "Connection to proxy host from which to start other connections. Cannot be overridden in resources and data sources.",
					Elem:        connectionSchemaResource,
				},
				"max_sessions": {
					Type:        schema.TypeInt,
					Optional:    true,
					Default:     3,
					Description: "Maximum number of open sessions in each host connection.",
				},
			},
		}

		p.ConfigureContextFunc = configure(version, p)

		return p
	}
}

type apiClient struct {
	resourceData   *schema.ResourceData
	mux            *sync.Mutex
	remoteClients  map[string]*RemoteClient
	activeSessions map[string]int
	maxSessions    int
}

func configure(version string, p *schema.Provider) func(context.Context, *schema.ResourceData) (interface{}, diag.Diagnostics) {
	return func(c context.Context, d *schema.ResourceData) (interface{}, diag.Diagnostics) {
		client := apiClient{
			resourceData:   d,
			maxSessions:    d.Get("max_sessions").(int),
			mux:            &sync.Mutex{},
			remoteClients:  map[string]*RemoteClient{},
			activeSessions: map[string]int{},
		}

		return &client, diag.Diagnostics{}
	}
}

func (c *apiClient) getConnWithDefault(d *schema.ResourceData) (*schema.ResourceData, error) {
	_, ok := d.GetOk("conn")
	if ok {
		return d, nil
	}

	c.mux.Lock()
	defer c.mux.Unlock()

	_, ok = c.resourceData.GetOk("conn")
	if ok {
		return c.resourceData, nil
	}

	return nil, errors.New("neither the provider nor the resource/data source have a configured connection")
}

func (c *apiClient) getRemoteClient(ctx context.Context, d *schema.ResourceData) (*RemoteClient, error) {
	connectionID := resourceConnectionHash(d)
	defer c.mux.Unlock()
	for {
		c.mux.Lock()

		client, ok := c.remoteClients[connectionID]
		if ok {
			if c.activeSessions[connectionID] >= c.maxSessions {
				c.mux.Unlock()
				continue
			}
			c.activeSessions[connectionID] += 1

			return client, nil
		}

		client, err := remoteClientFromResourceData(ctx, d)
		if err != nil {
			return nil, err
		}

		c.remoteClients[connectionID] = client
		c.activeSessions[connectionID] = 1
		return client, nil
	}
}

func remoteClientFromResourceData(ctx context.Context, d *schema.ResourceData) (*RemoteClient, error) {
	host, clientConfig, err := ConnectionFromResourceData(ctx, d)
	if err != nil {
		return nil, err
	}

	proxyHost, proxyClientConfig, err := ProxyConnectionFromResourceData(ctx, d)
	if err != nil {
		return nil, err
	}

	if proxyHost != "" && proxyClientConfig != nil {
		return NewRemoteProxyClient(host, clientConfig, proxyHost, proxyClientConfig)
	}

	return NewRemoteClient(host, clientConfig)
}

func (c *apiClient) closeRemoteClient(d *schema.ResourceData) error {
	connectionID := resourceConnectionHash(d)
	c.mux.Lock()
	defer c.mux.Unlock()

	c.activeSessions[connectionID] -= 1
	if c.activeSessions[connectionID] == 0 {
		client := c.remoteClients[connectionID]
		delete(c.remoteClients, connectionID)
		return client.Close()
	}

	return nil
}

func setResourceID(d *schema.ResourceData, conn *schema.ResourceData) {
	id := fmt.Sprintf("%s:%d:%s",
		conn.Get("conn.0.host").(string),
		conn.Get("conn.0.port").(int),
		d.Get("path").(string))

	proxy_host := resourceStringWithDefault(conn, "proxy_conn.0.host", "")
	proxy_port := resourceIntWithDefault(conn, "proxy_conn.0.port", "")

	if proxy_host != "" && proxy_port != "" {
		id = fmt.Sprintf("%s:%s|%s", proxy_host, proxy_port, id)
	}

	d.SetId(id)
}

func resourceConnectionHash(d *schema.ResourceData) string {
	elements := []string{
		d.Get("conn.0.host").(string),
		d.Get("conn.0.user").(string),
		strconv.Itoa(d.Get("conn.0.port").(int)),
		resourceStringWithDefault(d, "conn.0.password", ""),
		resourceStringWithDefault(d, "conn.0.private_key", ""),
		resourceStringWithDefault(d, "conn.0.private_key_path", ""),
		strconv.FormatBool(d.Get("conn.0.agent").(bool)),
		resourceStringWithDefault(d, "proxy_conn.0.host", ""),
		resourceStringWithDefault(d, "proxy_conn.0.user", ""),
		resourceIntWithDefault(d, "proxy_conn.0.port", ""),
		resourceStringWithDefault(d, "proxy_conn.0.password", ""),
		resourceStringWithDefault(d, "proxy_conn.0.private_key", ""),
		resourceStringWithDefault(d, "proxy_conn.0.private_key_path", ""),
		resourceBoolWithDefault(d, "proxy_conn.0.agent", ""),
	}

	return strings.Join(elements, "::")
}

func resourceStringWithDefault(d *schema.ResourceData, key string, defaultValue string) string {
	str, ok := d.GetOk(key)
	if ok {
		return str.(string)
	}
	return defaultValue
}

func resourceIntWithDefault(d *schema.ResourceData, key string, defaultValue string) string {
	integer, ok := d.GetOk(key)
	if ok {
		return strconv.Itoa(integer.(int))
	}
	return defaultValue
}

func resourceBoolWithDefault(d *schema.ResourceData, key string, defaultValue string) string {
	boolean, ok := d.GetOk(key)
	if ok {
		return strconv.FormatBool(boolean.(bool))
	}
	return defaultValue
}
