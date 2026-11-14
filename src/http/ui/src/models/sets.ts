export interface SetDomainMatch {
  domain: string;
  set_name: string;
  set_id: string;
  via: string;
  relation: string;
  entry: string;
  enabled: boolean;
  handles?: boolean;
}

export interface DomainReassignment {
  domain: string;
  set_name: string;
  set_id: string;
}

export interface CoveredSet {
  set_id: string;
  set_name: string;
  geosite: string[];
  geoip: string[];
  asns: string[];
  idle: boolean;
}
