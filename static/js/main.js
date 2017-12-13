var hostname = location.protocol + '//' + location.host;

$(document).ready(function() {
    // send message on button click
    $("form#upload-form").submit(function(e) {
        e.preventDefault();
        var formData = new FormData(this);
        performRequest(hostname + "/upload/", "POST", formData, function(html) {
            window.location = "/"
        });
    });
    
    // description
    $("#desc-search-input").on("input", performSearch);

    // tags
    performRequest(hostname + "/data?tags=true", "GET", "", function(data) {
        // tags & people
        $('#tags-search-input, #tags-input').tokenfield({
            autocomplete: {
                source: JSON.parse(data),
                delay: 0
            },
            showAutocompleteOnFocus: true,
            limit: 5
        }).on("tokenfield:createdtoken tokenfield:editedtoken tokenfield:removedtoken", performSearch);
    });

    // people
    performRequest(hostname + "/data?people=true", "GET", "", function(data) {
        // tags & people
        $('#people-search-input, #people-input').tokenfield({
            autocomplete: {
                source: JSON.parse(data),
                delay: 0
            },
            showAutocompleteOnFocus: true,
            limit: 5
        }).on("tokenfield:createdtoken tokenfield:editedtoken tokenfield:removedtoken", performSearch);
    });

    // media type drop down
    performRequest(hostname + "/data?file_types=true", "GET", "", function(data) {
        // tags & people
        $('#type-search-input').tokenfield({
            autocomplete: {
                source: JSON.parse(data),
                delay: 0
            },
            showAutocompleteOnFocus: true,
            limit: 5
        }).on("tokenfield:createdtoken tokenfield:editedtoken tokenfield:removedtoken", performSearch);
    });
    
    // date pickers
    $("#min-date-picker, #max-date-picker").datetimepicker({
        format: "DD/MM/YYYY"
    }).on("dp.change", performSearch);
    
});

// Perform search/filter request.
function performSearch() {
    var dates = [$("#min-date-picker").data("DateTimePicker").date(), $("#max-date-picker").data("DateTimePicker").date()];
    var tokenfieldTags = [$("#tags-search-input").tokenfield('getTokensList', ",", false), $("#people-search-input").tokenfield('getTokensList', ",", false), $("#type-search-input").tokenfield('getTokensList', ",", false)];
    var request = "/search?desc=" + $("#desc-search-input").val() + "&min_date=" + dates[0] + "&max_date=" + dates[1] + "&tags=" + tokenfieldTags[0] + "&people=" + tokenfieldTags[1] + "&file_types=" + tokenfieldTags[2] + "&format=html";
    
    console.log(request);
    performRequest(hostname + request, "GET", "", function(html) {
        $("#results-window").empty().append(html)
    });
}

// Perform AJAX request.
function performRequest(URL, httpMethod, data, resultMethod) {
    $.ajax({
        url: URL,
        type: httpMethod,
        dataType: 'text',
        data: data,
        error: function(e) {
            console.log(e);
        },
        success: function(e) {
            resultMethod(e);
        },
        cache: false,
        contentType: false,
        processData: false
    });
}