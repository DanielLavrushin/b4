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
