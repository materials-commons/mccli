
def _trunc(s, size=40):
    _s = str(s)
    if len(_s) > size:
        _s = _s[:size-3] + '...'
    return _s

def _trunc_name(obj, size=40):
    _name = obj.name
    if len(_name) > size:
        _name = _name[:size-3] + '...'
    return _name

def _trunc_desc(obj, size=100):
    _desc = obj.description
    if len(_desc) > size:
        _desc = _desc[:size-3] + '...'
    return _desc

def trunc(s, size=40):
    return _trunc(s, size)

class PrintFormatter(object):
    """Print dict-like records, nicely

    tabulate works nicely when you have all the records ahead of time, PrintFormatter lets you print without having all the records ahead of time.

    Arguments:
        fmt: List of tuples
            Specify how to format records via: (key, header, alignment, size, function)
                key: str, key being described
                header: str, header string
                alignment: str, string.format alignment option
                size: int, column size
                function: function, function that acts on record[key], or None if key is not in record, to determine what is printed.
    """
    def __init__(self, fmt, sep=" ", end="\n", truncate=True):
        self.fmt = fmt
        self.sep = sep
        self.end = end
        self.truncate = truncate

        self.fmtstr = ""
        for fmt_tuple in fmt:
            key = fmt_tuple[0]
            alignment = fmt_tuple[2]
            size = fmt_tuple[3]
            if self.truncate:
                self.fmtstr += "{" + key + ":" + alignment + str(size) + "}" + self.sep
            else:
                self.fmtstr += "{" + key + "}" + self.sep

        self.header_line = self.fmtstr.format(**{fmt_tuple[0]:fmt_tuple[1] for fmt_tuple in fmt})

        if self.truncate:
            self.header_sep = ""
            for fmt_tuple in fmt:
                size = fmt_tuple[3]
                self.header_sep += "-"*fmt_tuple[3] + self.sep

        def _if_key_in_record(key, record):
            return (key in record)

        def _get_key_from_record(key, record):
            return record[key]

        self.if_key_in_record = _if_key_in_record
        self.get_key_from_record = _get_key_from_record
        self.value_if_key_not_in_record = "-"

    def print_header(self):
        print(self.header_line, end=self.end)
        if self.truncate:
            print(self.header_sep, end=self.end)

    def _record_to_data(self, record):
        data = {}
        for fmt_tuple in self.fmt:
            key = fmt_tuple[0]
            size = fmt_tuple[3]
            f = fmt_tuple[4]
            value = None
            if self.if_key_in_record(key, record):
                value = self.get_key_from_record(key, record)
            if value is None and self.truncate:
                value = self.value_if_key_not_in_record
            else:
                if self.truncate:
                    value = trunc(f(value), size)
                else:
                    value = f(value)
            data[key] = value
        return data

    def print(self, record):
        print(self.fmtstr.format(**self._record_to_data(record)), end=self.end)

    def print_detail(self, title_key, record):
        data = self._record_to_data(record)

        print(str(data[title_key]) + ":")
        for fmt_tuple in self.fmt:
            key = fmt_tuple[0]
            print(key + ": '" + str(data[key]) + "'")
        print()
